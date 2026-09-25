package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lepro/suno-open-api/internal/model"
)

//go:embed schema.sql
var schemaSQL string

// EnsureMVTables 启动时补建长 MV、创作证明等后加的表并补齐后加的字段（含 tasks 的重试字段），已有库升级后无需手动执行迁移。
func (s *Store) EnsureMVTables(ctx context.Context) error {
	for _, stmt := range strings.Split(schemaSQL, ";") {
		stmt = strings.TrimSpace(stmt)
		if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS `mv_") ||
			strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS `certificates`") {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}

	// 早期建的表缺少后加的字段，逐个补齐
	for _, c := range mvAddedColumns {
		var n int
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, c.table, c.column).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", c.table, c.column, c.ddl)); err != nil {
				return err
			}
		}
	}
	return nil
}

var mvAddedColumns = []struct{ table, column, ddl string }{
	{"tasks", "retry_count", "INT NOT NULL DEFAULT 0 AFTER `points_refunded`"},
	{"tasks", "retried_at", "DATETIME NULL AFTER `retry_count`"},
	{"mv_projects", "tier", "VARCHAR(16) NOT NULL DEFAULT 'premium' AFTER `mode`"},
	{"mv_projects", "reuse_chorus", "TINYINT NOT NULL DEFAULT 0 AFTER `tier`"},
	{"mv_projects", "subtitles", "TINYINT NOT NULL DEFAULT 0 AFTER `reuse_chorus`"},
	{"mv_segments", "kind", "VARCHAR(8) NOT NULL DEFAULT 'video' AFTER `seq`"},
	{"mv_segments", "reuse_of", "INT NULL AFTER `kind`"},
	{"mv_segments", "section", "VARCHAR(64) NOT NULL DEFAULT '' AFTER `reuse_of`"},
	{"mv_projects", "look", "TEXT NULL AFTER `visual_bible`"},
	{"mv_segments", "keyframe_url", "VARCHAR(1024) NOT NULL DEFAULT '' AFTER `prompt`"},
}

const mvProjectColumns = `p.id, p.merchant_id, m.name, p.song_task_id, p.title, p.mode, p.tier, p.reuse_chorus,
	p.subtitles, p.status, p.style_note,
	p.visual_bible, p.look, p.writer, p.ratio, p.resolution, p.use_cover, p.cover_url, p.audio_url, p.duration,
	p.points_cost, p.points_refunded, p.video_key, p.error_message, p.created_by,
	p.created_at, p.updated_at, p.started_at, p.finished_at,
	(SELECT COUNT(*) FROM mv_segments s WHERE s.project_id = p.id),
	(SELECT COUNT(*) FROM mv_segments s WHERE s.project_id = p.id AND s.status = 'completed')`

func scanMVProject(row interface{ Scan(...interface{}) error }) (*model.MVProject, error) {
	var p model.MVProject
	var bible, look sql.NullString
	var started, finished sql.NullTime
	err := row.Scan(&p.ID, &p.MerchantID, &p.MerchantName, &p.SongTaskID, &p.Title, &p.Mode, &p.Tier,
		&p.ReuseChorus, &p.Subtitles, &p.Status,
		&p.StyleNote, &bible, &look, &p.Writer, &p.Ratio, &p.Resolution, &p.UseCover, &p.CoverURL, &p.AudioURL,
		&p.Duration, &p.PointsCost, &p.PointsRefunded, &p.VideoKey, &p.ErrorMessage, &p.CreatedBy,
		&p.CreatedAt, &p.UpdatedAt, &started, &finished, &p.SegmentTotal, &p.SegmentDone)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.VisualBible = bible.String
	if look.String != "" {
		_ = json.Unmarshal([]byte(look.String), &p.Look)
	}
	if started.Valid {
		p.StartedAt = &started.Time
	}
	if finished.Valid {
		p.FinishedAt = &finished.Time
	}
	return &p, nil
}

// CreateMVProject 新建项目及其分镜片段。
func (s *Store) CreateMVProject(ctx context.Context, p *model.MVProject, segs []*model.MVSegment) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO mv_projects (merchant_id, song_task_id, title, mode, tier, reuse_chorus, subtitles, status,
		                         style_note, visual_bible, look, writer, ratio, resolution, use_cover, cover_url,
		                         audio_url, duration, points_cost, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.MerchantID, p.SongTaskID, p.Title, p.Mode, p.Tier, p.ReuseChorus, p.Subtitles, model.MVDraft,
		p.StyleNote, p.VisualBible, lookJSON(p.Look), p.Writer, p.Ratio, p.Resolution, p.UseCover, p.CoverURL,
		p.AudioURL, p.Duration, p.PointsCost, p.CreatedBy)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()

	for _, seg := range segs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mv_segments (project_id, seq, kind, reuse_of, section, start_s, end_s, lyrics, prompt)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, seg.Seq, seg.Kind, seg.ReuseOf, seg.Section, seg.Start, seg.End, seg.Lyrics, seg.Prompt); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func lookJSON(l model.MVLook) string {
	l.RefURLs = nil
	if l.RefKeys == nil {
		l.RefKeys = []string{}
	}
	raw, _ := json.Marshal(l)
	return string(raw)
}

// UpdateMVLook 更新草稿的画面要求、形象设定与报价（参考图增减会改变视频镜头是否先画首帧）。
func (s *Store) UpdateMVLook(ctx context.Context, projectID int64, styleNote string, look model.MVLook, price int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE mv_projects SET style_note = ?, look = ?, points_cost = ? WHERE id = ? AND status = ?`,
		styleNote, lookJSON(look), price, projectID, model.MVDraft)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var status string
		if err := s.db.QueryRowContext(ctx, `SELECT status FROM mv_projects WHERE id = ?`, projectID).Scan(&status); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		if status != model.MVDraft {
			return fmt.Errorf("只有草稿可以修改形象设定")
		}
	}
	return nil
}

// MVProjectByID 查询项目（不含片段）。
func (s *Store) MVProjectByID(ctx context.Context, id int64) (*model.MVProject, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+mvProjectColumns+`
		FROM mv_projects p JOIN merchants m ON m.id = p.merchant_id WHERE p.id = ?`, id)
	return scanMVProject(row)
}

// ListMVProjects 分页列出项目，merchantID 为 0 时返回全部。
func (s *Store) ListMVProjects(ctx context.Context, merchantID int64, page, size int) ([]*model.MVProject, int64, error) {
	where, args := "1=1", []interface{}{}
	if merchantID > 0 {
		where, args = "p.merchant_id = ?", append(args, merchantID)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mv_projects p WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, size = normalizePaging(page, size)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s
		FROM mv_projects p JOIN merchants m ON m.id = p.merchant_id
		WHERE %s ORDER BY p.id DESC LIMIT ? OFFSET ?`, mvProjectColumns, where),
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]*model.MVProject, 0, size)
	for rows.Next() {
		p, err := scanMVProject(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, p)
	}
	return list, total, rows.Err()
}

// MVProjectsByStatus 返回处于指定状态的项目，供后台 worker 推进。
func (s *Store) MVProjectsByStatus(ctx context.Context, statuses ...string) ([]*model.MVProject, error) {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := make([]interface{}, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+mvProjectColumns+`
		FROM mv_projects p JOIN merchants m ON m.id = p.merchant_id
		WHERE p.status IN (`+marks+`) ORDER BY p.id LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*model.MVProject
	for rows.Next() {
		p, err := scanMVProject(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// MVSegments 按序号返回项目的全部片段。
func (s *Store) MVSegments(ctx context.Context, projectID int64) ([]*model.MVSegment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, seq, kind, reuse_of, section, start_s, end_s, lyrics, prompt, keyframe_url, status, provider_task_id,
		       video_url, attempts, error_message, updated_at
		FROM mv_segments WHERE project_id = ? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*model.MVSegment
	for rows.Next() {
		var seg model.MVSegment
		var lyrics, prompt sql.NullString
		var reuseOf sql.NullInt64
		if err := rows.Scan(&seg.ID, &seg.ProjectID, &seg.Seq, &seg.Kind, &reuseOf, &seg.Section,
			&seg.Start, &seg.End, &lyrics, &prompt, &seg.KeyframeURL,
			&seg.Status, &seg.ProviderTaskID, &seg.VideoURL, &seg.Attempts, &seg.ErrorMessage,
			&seg.UpdatedAt); err != nil {
			return nil, err
		}
		seg.Lyrics, seg.Prompt = lyrics.String, prompt.String
		if reuseOf.Valid {
			n := int(reuseOf.Int64)
			seg.ReuseOf = &n
		}
		list = append(list, &seg)
	}
	return list, rows.Err()
}

// UpdateMVStoryboard 更新草稿的整体设定与分镜提示词，仅草稿可改。
func (s *Store) UpdateMVStoryboard(ctx context.Context, projectID int64, bible, writer string, prompts map[int64]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE mv_projects SET visual_bible = ?, writer = IF(? = '', writer, ?) WHERE id = ? AND status = ?`,
		bible, writer, writer, projectID, model.MVDraft)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 内容未变时 RowsAffected 也为 0，再确认一次状态
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM mv_projects WHERE id = ?`, projectID).Scan(&status); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		if status != model.MVDraft {
			return fmt.Errorf("只有草稿可以修改分镜")
		}
	}
	for segID, prompt := range prompts {
		if _, err := tx.ExecContext(ctx,
			`UPDATE mv_segments SET prompt = ? WHERE id = ? AND project_id = ?`, prompt, segID, projectID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// StartMVProject 在同一事务中扣除积分并把草稿置为生成中；余额不足时返回 ErrInsufficientPoints。
func (s *Store) StartMVProject(ctx context.Context, projectID, merchantID, cost int64, remark string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM mv_projects WHERE id = ? FOR UPDATE`, projectID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if status != model.MVDraft {
		return 0, fmt.Errorf("该 MV 已开始生成")
	}

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT points FROM merchants WHERE id = ? FOR UPDATE`, merchantID).Scan(&balance); err != nil {
		return 0, err
	}
	if balance < cost {
		return balance, ErrInsufficientPoints
	}
	balance -= cost
	if _, err := tx.ExecContext(ctx, `UPDATE merchants SET points = ? WHERE id = ?`, balance, merchantID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO point_logs (merchant_id, type, points, balance, remark) VALUES (?, ?, ?, ?, ?)`,
		merchantID, model.PointConsume, -cost, balance, remark); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mv_projects SET status = ?, points_cost = ?, started_at = NOW(), error_message = '' WHERE id = ?`,
		model.MVGenerating, cost, projectID); err != nil {
		return 0, err
	}
	// 复用镜头不需要生成，直接计为完成，合成时取被复用镜头的素材
	if _, err := tx.ExecContext(ctx, `
		UPDATE mv_segments SET status = ? WHERE project_id = ? AND kind = ?`,
		model.SegCompleted, projectID, model.ShotReuse); err != nil {
		return 0, err
	}
	return balance, tx.Commit()
}

// UpdateMVSegment 写回片段的执行状态。
func (s *Store) UpdateMVSegment(ctx context.Context, seg *model.MVSegment) error {
	msg := seg.ErrorMessage
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE mv_segments SET status = ?, provider_task_id = ?, video_url = ?, keyframe_url = ?, attempts = ?,
		       error_message = ?
		WHERE id = ?`, seg.Status, seg.ProviderTaskID, seg.VideoURL, seg.KeyframeURL, seg.Attempts, msg, seg.ID)
	return err
}

// SetMVStatus 更新项目状态；进入终态时记录完成时间。
func (s *Store) SetMVStatus(ctx context.Context, projectID int64, status, videoKey, errMsg string) error {
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	finished := "finished_at"
	if status == model.MVCompleted || status == model.MVFailed {
		finished = "NOW()"
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE mv_projects SET status = ?, video_key = IF(? = '', video_key, ?), error_message = ?,
		       finished_at = `+finished+` WHERE id = ?`,
		status, videoKey, videoKey, errMsg, projectID)
	return err
}

// RefundMVProject 退还项目积分，同一项目只退一次。
func (s *Store) RefundMVProject(ctx context.Context, projectID int64, remark string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var merchantID, cost int64
	var refunded bool
	if err := tx.QueryRowContext(ctx, `
		SELECT merchant_id, points_cost, points_refunded FROM mv_projects WHERE id = ? FOR UPDATE`,
		projectID).Scan(&merchantID, &cost, &refunded); err != nil {
		return err
	}
	if refunded || cost <= 0 {
		return nil
	}

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT points FROM merchants WHERE id = ? FOR UPDATE`, merchantID).Scan(&balance); err != nil {
		return err
	}
	balance += cost
	if _, err := tx.ExecContext(ctx, `UPDATE merchants SET points = ? WHERE id = ?`, balance, merchantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO point_logs (merchant_id, type, points, balance, remark) VALUES (?, ?, ?, ?, ?)`,
		merchantID, model.PointRefund, cost, balance, remark); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE mv_projects SET points_refunded = 1 WHERE id = ?`, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteMVProject 删除项目，片段随外键级联删除。
func (s *Store) DeleteMVProject(ctx context.Context, projectID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mv_projects WHERE id = ?`, projectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RetryMVProject 重试失败的项目：按 cost 扣费（0 为免费），保留已完成的片段，其余片段重新生成。
func (s *Store) RetryMVProject(ctx context.Context, projectID, merchantID, cost int64, remark string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM mv_projects WHERE id = ? FOR UPDATE`, projectID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if status != model.MVFailed {
		return 0, fmt.Errorf("只有失败的 MV 可以重试")
	}

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT points FROM merchants WHERE id = ? FOR UPDATE`, merchantID).Scan(&balance); err != nil {
		return 0, err
	}
	// cost 为 0 表示免费重试（失败时未退款），不扣费也不记流水
	if cost > 0 {
		if balance < cost {
			return balance, ErrInsufficientPoints
		}
		balance -= cost
		if _, err := tx.ExecContext(ctx, `UPDATE merchants SET points = ? WHERE id = ?`, balance, merchantID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO point_logs (merchant_id, type, points, balance, remark) VALUES (?, ?, ?, ?, ?)`,
			merchantID, model.PointConsume, -cost, balance, remark); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mv_projects SET status = ?, points_refunded = 0, started_at = NOW(),
		       finished_at = NULL, error_message = '' WHERE id = ?`,
		model.MVGenerating, projectID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mv_segments SET status = ?, attempts = 0, provider_task_id = '', keyframe_url = '', error_message = ''
		WHERE project_id = ? AND kind <> ? AND status <> ?`,
		model.SegPending, projectID, model.ShotReuse, model.SegCompleted); err != nil {
		return 0, err
	}
	return balance, tx.Commit()
}

// ResetMVSegments 把指定片段重置为待生成（例如素材链接已过期），项目回到生成中。
func (s *Store) ResetMVSegments(ctx context.Context, projectID int64, seqs []int, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, seq := range seqs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE mv_segments SET status = ?, attempts = 0, provider_task_id = '', video_url = '', keyframe_url = '',
			       error_message = ?
			WHERE project_id = ? AND seq = ?`, model.SegPending, reason, projectID, seq); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE mv_projects SET status = ? WHERE id = ?`, model.MVGenerating, projectID); err != nil {
		return err
	}
	return tx.Commit()
}
