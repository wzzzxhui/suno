package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/lepro/suno-open-api/internal/model"
)

// ErrCertificateNoTaken 证书编号撞号（概率极低），调用方换一个编号重试。
var ErrCertificateNoTaken = errors.New("证书编号已被占用")

const certificateColumns = `id, cert_no, merchant_id, task_id, suno_id, title, author, lyrics, tags, model_name,
	duration, audio_sha256, audio_size, song_created_at, points_cost, created_at`

func scanCertificate(row scanner) (*model.Certificate, error) {
	var c model.Certificate
	var lyrics sql.NullString
	err := row.Scan(&c.ID, &c.No, &c.MerchantID, &c.TaskID, &c.SunoID, &c.Title, &c.Author, &lyrics, &c.Tags,
		&c.ModelName, &c.Duration, &c.AudioSHA256, &c.AudioSize, &c.SongCreatedAt, &c.PointsCost, &c.IssuedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Lyrics = lyrics.String
	return &c, nil
}

// CertificateByNo 按证书编号查询。
func (s *Store) CertificateByNo(ctx context.Context, no string) (*model.Certificate, error) {
	return scanCertificate(s.db.QueryRowContext(ctx,
		`SELECT `+certificateColumns+` FROM certificates WHERE cert_no = ?`, no))
}

// CertificateByTask 查询作品已签发的证明。
func (s *Store) CertificateByTask(ctx context.Context, taskID int64) (*model.Certificate, error) {
	return scanCertificate(s.db.QueryRowContext(ctx,
		`SELECT `+certificateColumns+` FROM certificates WHERE task_id = ?`, taskID))
}

// SongTaskByCustomID 按 custom_id 找商户的作品任务。格式下载等衍生任务也会带同一个 custom_id，
// 这里排除掉，只取产出这首歌的原始任务。
func (s *Store) SongTaskByCustomID(ctx context.Context, merchantID int64, customID string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM tasks
		WHERE merchant_id = ? AND custom_id = ? AND status = 'completed'
		  AND kind NOT IN ('video', 'aligned_lyrics', 'voice_train', 'download_wav', 'download_mp3', 'download_m4a')
		ORDER BY id ASC LIMIT 1`, merchantID, customID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return id, err
}

// IssueCertificate 在一个事务里扣费并写入证明，返回扣费后余额。
// 作品已有证明时不扣费，直接返回已有的那份（existing 为 true）。
// 先锁商户行：同一商户的并发签发会排队，不会对同一作品重复扣费。
func (s *Store) IssueCertificate(ctx context.Context, c *model.Certificate, remark string) (
	cert *model.Certificate, existing bool, balance int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := tx.QueryRowContext(ctx,
		`SELECT points FROM merchants WHERE id = ? FOR UPDATE`, c.MerchantID).Scan(&balance); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, 0, ErrNotFound
		}
		return nil, false, 0, err
	}

	prev, err := scanCertificate(tx.QueryRowContext(ctx,
		`SELECT `+certificateColumns+` FROM certificates WHERE task_id = ?`, c.TaskID))
	if err == nil {
		return prev, true, balance, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, 0, err
	}

	if balance < c.PointsCost {
		return nil, false, balance, ErrInsufficientPoints
	}
	if c.PointsCost > 0 {
		balance -= c.PointsCost
		if _, err := tx.ExecContext(ctx, `UPDATE merchants SET points = ? WHERE id = ?`, balance, c.MerchantID); err != nil {
			return nil, false, 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO point_logs (merchant_id, type, points, balance, remark, task_id) VALUES (?, ?, ?, ?, ?, ?)`,
			c.MerchantID, model.PointConsume, -c.PointsCost, balance, remark, c.TaskID); err != nil {
			return nil, false, 0, err
		}
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO certificates (cert_no, merchant_id, task_id, suno_id, title, author, lyrics, tags, model_name,
		                          duration, audio_sha256, audio_size, song_created_at, points_cost, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.No, c.MerchantID, c.TaskID, c.SunoID, c.Title, c.Author, nullString(c.Lyrics), c.Tags, c.ModelName,
		c.Duration, c.AudioSHA256, c.AudioSize, c.SongCreatedAt, c.PointsCost, c.IssuedAt)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return nil, false, 0, ErrCertificateNoTaken
		}
		return nil, false, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, 0, err
	}
	c.ID, _ = res.LastInsertId()
	return c, false, balance, nil
}
