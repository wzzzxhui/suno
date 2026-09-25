package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

/* ---------------------------------- 管理员账号 ---------------------------------- */

// CreateAdmin 新建后台账号。
func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash, nickname, role string) (*model.AdminUser, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_users (username, password_hash, nickname, role, status) VALUES (?, ?, ?, ?, 1)`,
		username, passwordHash, nickname, role)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.AdminUser{ID: id, Username: username, Nickname: nickname, Role: role, Status: 1}, nil
}

// AdminByUsername 按用户名查询后台账号。
func (s *Store) AdminByUsername(ctx context.Context, username string) (*model.AdminUser, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, nickname, role, status, last_login_at, created_at
		FROM admin_users WHERE username = ?`, username)
	return scanAdmin(row)
}

// AdminByID 按主键查询后台账号。
func (s *Store) AdminByID(ctx context.Context, id int64) (*model.AdminUser, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, nickname, role, status, last_login_at, created_at
		FROM admin_users WHERE id = ?`, id)
	return scanAdmin(row)
}

func scanAdmin(row *sql.Row) (*model.AdminUser, error) {
	var (
		u         model.AdminUser
		lastLogin sql.NullTime
	)
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Nickname, &u.Role, &u.Status, &lastLogin, &u.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if lastLogin.Valid {
		t := lastLogin.Time
		u.LastLoginAt = &t
	}
	return &u, nil
}

// TouchAdminLogin 记录登录时间。
func (s *Store) TouchAdminLogin(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE admin_users SET last_login_at = NOW() WHERE id = ?`, id)
}

// UpdateAdminPassword 修改后台账号密码。
func (s *Store) UpdateAdminPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

/* ---------------------------------- 商户管理 ---------------------------------- */

// ListMerchants 分页查询商户，并带出密钥数、任务数与累计消耗。
func (s *Store) ListMerchants(ctx context.Context, keyword string, status, page, size int) ([]model.MerchantRow, int64, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}

	if keyword != "" {
		where = append(where, "(m.name LIKE ? OR m.id = ?)")
		args = append(args, "%"+keyword+"%", keyword)
	}
	if status == 0 || status == 1 {
		where = append(where, "m.status = ?")
		args = append(args, status)
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM merchants m WHERE %s`, clause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, size = normalizePaging(page, size)
	listArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id, m.name, m.status, m.points, m.created_at, m.updated_at,
		       (SELECT COUNT(*) FROM api_keys k WHERE k.merchant_id = m.id) AS key_count,
		       (SELECT COUNT(*) FROM tasks t WHERE t.merchant_id = m.id) AS task_count,
		       COALESCE((SELECT SUM(-l.points) FROM point_logs l
		                 WHERE l.merchant_id = m.id AND l.type = 1), 0) AS consumed_total,
		       (SELECT MAX(t.created_at) FROM tasks t WHERE t.merchant_id = m.id) AS last_active_at
		FROM merchants m
		WHERE %s
		ORDER BY m.id DESC
		LIMIT ? OFFSET ?`, clause), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]model.MerchantRow, 0, size)
	for rows.Next() {
		var (
			r      model.MerchantRow
			active sql.NullTime
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Status, &r.Points, &r.CreatedAt, &r.UpdatedAt,
			&r.KeyCount, &r.TaskCount, &r.ConsumedTotal, &active); err != nil {
			return nil, 0, err
		}
		if active.Valid {
			t := active.Time
			r.LastActiveAt = &t
		}
		list = append(list, r)
	}
	return list, total, rows.Err()
}

// SetMerchantStatus 启用或禁用商户。
func (s *Store) SetMerchantStatus(ctx context.Context, id int64, status int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE merchants SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	return confirmAffected(ctx, s.db, res, "merchants", id)
}

// RenameMerchant 修改商户名称。
func (s *Store) RenameMerchant(ctx context.Context, id int64, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE merchants SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	return confirmAffected(ctx, s.db, res, "merchants", id)
}

/* ---------------------------------- 密钥管理 ---------------------------------- */

// APIKeyRow 后台展示用的密钥行。
type APIKeyRow struct {
	model.APIKey
	MerchantName string `json:"merchant_name"`
}

// ListAPIKeys 查询密钥列表，merchantID 为 0 时查全部。
func (s *Store) ListAPIKeys(ctx context.Context, merchantID int64, page, size int) ([]APIKeyRow, int64, error) {
	where := "1 = 1"
	args := []interface{}{}
	if merchantID > 0 {
		where = "k.merchant_id = ?"
		args = append(args, merchantID)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM api_keys k WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, size = normalizePaging(page, size)
	listArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT k.id, k.merchant_id, k.name, k.key_prefix, k.status, k.last_used_at, k.created_at, m.name
		FROM api_keys k
		JOIN merchants m ON m.id = k.merchant_id
		WHERE %s
		ORDER BY k.id DESC
		LIMIT ? OFFSET ?`, where), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]APIKeyRow, 0, size)
	for rows.Next() {
		var (
			r        APIKeyRow
			lastUsed sql.NullTime
		)
		if err := rows.Scan(&r.ID, &r.MerchantID, &r.Name, &r.Prefix, &r.Status,
			&lastUsed, &r.CreatedAt, &r.MerchantName); err != nil {
			return nil, 0, err
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			r.LastUsedAt = &t
		}
		list = append(list, r)
	}
	return list, total, rows.Err()
}

// SetAPIKeyStatus 启用或禁用密钥。
func (s *Store) SetAPIKeyStatus(ctx context.Context, id int64, status int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	return confirmAffected(ctx, s.db, res, "api_keys", id)
}

// DeleteAPIKey 删除密钥。
func (s *Store) DeleteAPIKey(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

/* ---------------------------------- 任务管理 ---------------------------------- */

// ListTasks 按条件分页查询任务（跨商户）。
func (s *Store) ListTasks(ctx context.Context, f model.TaskFilter) ([]model.TaskRow, int64, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}

	if f.MerchantID > 0 {
		where = append(where, "t.merchant_id = ?")
		args = append(args, f.MerchantID)
	}
	if f.Kind != "" {
		where = append(where, "t.kind = ?")
		args = append(args, f.Kind)
	}
	if f.Status != "" {
		where = append(where, "t.status = ?")
		args = append(args, f.Status)
	}
	if f.Keyword != "" {
		where = append(where, "(t.custom_id LIKE ? OR t.id = ? OR t.provider_task_id LIKE ?)")
		args = append(args, "%"+f.Keyword+"%", f.Keyword, "%"+f.Keyword+"%")
	}
	if f.Start != "" {
		where = append(where, "t.created_at >= ?")
		args = append(args, f.Start)
	}
	if f.End != "" {
		where = append(where, "t.created_at <= ?")
		args = append(args, f.End)
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM tasks t WHERE %s`, clause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, size := normalizePaging(f.Page, f.Size)
	listArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.id, t.merchant_id, t.kind, t.status, t.provider_task_id, t.custom_id, t.proxy_url,
		       t.file_info, t.extend, t.extra_param, t.error_message, t.points_cost, t.points_refunded,
		       t.created_at, t.updated_at, t.finished_at, t.retry_count, t.retried_at, m.name
		FROM tasks t
		JOIN merchants m ON m.id = t.merchant_id
		WHERE %s
		ORDER BY t.id DESC
		LIMIT ? OFFSET ?`, clause), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]model.TaskRow, 0, size)
	for rows.Next() {
		row, err := scanTaskRow(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *row)
	}
	return list, total, rows.Err()
}

// AdminTaskByID 后台查看任务详情，额外返回原始请求参数。
func (s *Store) AdminTaskByID(ctx context.Context, id int64) (*model.TaskRow, string, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.merchant_id, t.kind, t.status, t.provider_task_id, t.custom_id, t.proxy_url,
		       t.file_info, t.extend, t.extra_param, t.error_message, t.points_cost, t.points_refunded,
		       t.created_at, t.updated_at, t.finished_at, t.retry_count, t.retried_at, m.name, t.request_payload
		FROM tasks t
		JOIN merchants m ON m.id = t.merchant_id
		WHERE t.id = ?`, id)

	var (
		t        model.TaskRow
		kind     string
		status   string
		customID sql.NullString
		fileInfo sql.NullString
		extend   sql.NullString
		finished sql.NullTime
		retried  sql.NullTime
		request  sql.NullString
	)
	err := row.Scan(&t.ID, &t.MerchantID, &kind, &status, &t.ProviderTaskID, &customID, &t.ProxyURL,
		&fileInfo, &extend, &t.ExtraParam, &t.ErrorMessage, &t.PointsCost, &t.PointsRefunded,
		&t.CreatedAt, &t.UpdatedAt, &finished, &t.RetryCount, &retried, &t.MerchantName, &request)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}

	fillTaskRow(&t, kind, status, customID, fileInfo, extend, finished, retried)
	return &t, request.String, nil
}

func scanTaskRow(rows *sql.Rows) (*model.TaskRow, error) {
	var (
		t        model.TaskRow
		kind     string
		status   string
		customID sql.NullString
		fileInfo sql.NullString
		extend   sql.NullString
		finished sql.NullTime
		retried  sql.NullTime
	)
	err := rows.Scan(&t.ID, &t.MerchantID, &kind, &status, &t.ProviderTaskID, &customID, &t.ProxyURL,
		&fileInfo, &extend, &t.ExtraParam, &t.ErrorMessage, &t.PointsCost, &t.PointsRefunded,
		&t.CreatedAt, &t.UpdatedAt, &finished, &t.RetryCount, &retried, &t.MerchantName)
	if err != nil {
		return nil, err
	}
	fillTaskRow(&t, kind, status, customID, fileInfo, extend, finished, retried)
	return &t, nil
}

func fillTaskRow(t *model.TaskRow, kind, status string, customID, fileInfo, extend sql.NullString, finished, retried sql.NullTime) {
	if retried.Valid {
		r := retried.Time
		t.RetriedAt = &r
	}
	t.Task.Kind = model.TaskKind(kind)
	t.Kind = kind
	t.KindLabel = model.LabelOf(model.TaskKind(kind))
	t.Status = model.TaskStatus(status)
	t.StatusCode = t.Status.Code()

	if customID.Valid {
		v := customID.String
		t.CustomID = &v
	}
	if extend.Valid {
		t.Extend = extend.String
	}
	if finished.Valid {
		f := finished.Time
		t.FinishedAt = &f
	}
	t.FileInfo = parseFileInfo(fileInfo)
}

/* ---------------------------------- 积分流水 ---------------------------------- */

// ListPointLogs 按条件分页查询积分流水（跨商户）。
func (s *Store) ListPointLogs(ctx context.Context, f model.PointLogFilter) ([]model.PointLogRow, int64, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}

	if f.MerchantID > 0 {
		where = append(where, "l.merchant_id = ?")
		args = append(args, f.MerchantID)
	}
	if f.Type > 0 {
		where = append(where, "l.type = ?")
		args = append(args, f.Type)
	}
	if f.Start != "" {
		where = append(where, "l.created_at >= ?")
		args = append(args, f.Start)
	}
	if f.End != "" {
		where = append(where, "l.created_at <= ?")
		args = append(args, f.End)
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM point_logs l WHERE %s`, clause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, size := normalizePaging(f.Page, f.Size)
	listArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT l.id, l.merchant_id, l.type, l.points, l.balance, l.remark, l.task_id, l.created_at, m.name
		FROM point_logs l
		JOIN merchants m ON m.id = l.merchant_id
		WHERE %s
		ORDER BY l.id DESC
		LIMIT ? OFFSET ?`, clause), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]model.PointLogRow, 0, size)
	for rows.Next() {
		var r model.PointLogRow
		if err := rows.Scan(&r.ID, &r.MerchantID, &r.Type, &r.Points, &r.Balance,
			&r.Remark, &r.TaskID, &r.CreatedAt, &r.MerchantName); err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	return list, total, rows.Err()
}

/* ---------------------------------- 作品库 ---------------------------------- */

// ListSongs 列出已完成且带 custom_id 的作品，并带出最近一次 MV 生成结果。
// MV 任务通过其请求参数里的 suno_id 与作品关联。
func (s *Store) ListSongs(ctx context.Context, f model.SongFilter) ([]model.SongRow, int64, error) {
	where := []string{"t.status = 'completed'", "t.custom_id IS NOT NULL", "t.kind <> 'video'"}
	args := []interface{}{}

	if f.MerchantID > 0 {
		where = append(where, "t.merchant_id = ?")
		args = append(args, f.MerchantID)
	}
	if f.Keyword != "" {
		where = append(where,
			"(t.custom_id LIKE ? OR JSON_UNQUOTE(JSON_EXTRACT(t.request_payload, '$.title')) LIKE ?)")
		args = append(args, "%"+f.Keyword+"%", "%"+f.Keyword+"%")
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM tasks t WHERE %s`, clause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, size := normalizePaging(f.Page, f.Size)
	listArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.id, t.merchant_id, m.name, t.custom_id, t.kind, t.file_info, t.proxy_url, t.created_at,
		       JSON_UNQUOTE(JSON_EXTRACT(t.request_payload, '$.title')) AS title,
		       v.id, v.status, v.proxy_url, v.file_info, v.error_message, c.cert_no
		FROM tasks t
		JOIN merchants m ON m.id = t.merchant_id
		LEFT JOIN certificates c ON c.task_id = t.id
		LEFT JOIN tasks v ON v.id = (
			SELECT vv.id FROM tasks vv
			WHERE vv.kind = 'video'
			  AND JSON_UNQUOTE(JSON_EXTRACT(vv.request_payload, '$.suno_id')) = t.custom_id
			ORDER BY vv.id DESC LIMIT 1
		)
		WHERE %s
		ORDER BY t.id DESC
		LIMIT ? OFFSET ?`, clause), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]model.SongRow, 0, size)
	for rows.Next() {
		var (
			row          model.SongRow
			customID     sql.NullString
			fileInfo     sql.NullString
			title        sql.NullString
			videoID      sql.NullInt64
			videoStatus  sql.NullString
			videoProxy   sql.NullString
			videoFile    sql.NullString
			videoMessage sql.NullString
			certNo       sql.NullString
		)
		if err := rows.Scan(&row.TaskID, &row.MerchantID, &row.MerchantName, &customID, &row.Kind,
			&fileInfo, &row.ProxyURL, &row.CreatedAt, &title,
			&videoID, &videoStatus, &videoProxy, &videoFile, &videoMessage, &certNo); err != nil {
			return nil, 0, err
		}

		row.CustomID = customID.String
		row.CertificateNo = certNo.String
		row.KindLabel = model.LabelOf(model.TaskKind(row.Kind))
		row.Title = strings.TrimSpace(title.String)
		if row.Title == "" || row.Title == "null" {
			row.Title = "未命名作品"
		}
		if info := parseFileInfo(fileInfo); info != nil {
			row.FileInfo = info
		}

		if videoID.Valid {
			id := videoID.Int64
			row.VideoTaskID = &id
			row.VideoStatus = videoStatus.String
			row.VideoMessage = videoMessage.String
			// 播放优先用签名代理地址；代理有效期约一天，过期后改用 mp4 原始地址
			if model.ProxyAlive(videoProxy.String, time.Now()) {
				row.VideoURL = videoProxy.String
			}
			if row.VideoURL == "" {
				if info := parseFileInfo(videoFile); info != nil {
					row.VideoURL = info.MP4URL
				}
			}
		}
		list = append(list, row)
	}
	return list, total, rows.Err()
}

// parseFileInfo 把数据库里的 JSON 解析成文件信息。
// fileSigner 把转存到平台 COS 的对象路径签成可访问地址，启动时设置；为 nil 时不替换。
var fileSigner func(key string) string

// SetFileSigner 设置转存副本的签名函数。
func SetFileSigner(fn func(key string) string) { fileSigner = fn }

// parseFileInfo 解析 file_info；作品已转存时，音频与封面换成平台副本的签名地址。
// 上游地址约一天后失效，平台副本长期可用。
func parseFileInfo(raw sql.NullString) *model.FileInfo {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var info model.FileInfo
	if err := json.Unmarshal([]byte(raw.String), &info); err != nil {
		return nil
	}
	var extra struct {
		Archived *model.ArchivedFiles `json:"archived"`
	}
	if json.Unmarshal([]byte(raw.String), &extra) == nil && extra.Archived != nil {
		info.Archived = extra.Archived
		if fileSigner != nil {
			if extra.Archived.Audio != "" {
				info.MP3URL, info.M4AURL, info.WAVURL = fileSigner(extra.Archived.Audio), "", ""
			}
			if extra.Archived.Cover != "" {
				info.CoverURL = fileSigner(extra.Archived.Cover)
			}
		}
	}
	return &info
}

// SetArchived 记录作品在平台 COS 中的副本。
func (s *Store) SetArchived(ctx context.Context, taskID int64, a model.ArchivedFiles) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE tasks SET file_info = JSON_SET(COALESCE(file_info, JSON_OBJECT()), '$.archived', CAST(? AS JSON))
		WHERE id = ?`, string(raw), taskID)
	return err
}

// UnarchivedSongs 返回尚未转存的已完成作品，按新到旧，供补转存使用。
func (s *Store) UnarchivedSongs(ctx context.Context, kinds []model.TaskKind, limit int) ([]int64, error) {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
	args := make([]interface{}, 0, len(kinds)+1)
	for _, k := range kinds {
		args = append(args, string(k))
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM tasks
		WHERE status = 'completed' AND kind IN (`+marks+`)
		  AND file_info IS NOT NULL AND JSON_EXTRACT(file_info, '$.archived') IS NULL
		ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

/* ---------------------------------- 统计 ---------------------------------- */

// Overview 汇总仪表盘所需的各项指标。
func (s *Store) Overview(ctx context.Context) (*model.Overview, error) {
	var o model.Overview

	row := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM merchants),
			(SELECT COUNT(*) FROM merchants WHERE status = 1),
			(SELECT COUNT(*) FROM api_keys WHERE status = 1),
			(SELECT COALESCE(SUM(points), 0) FROM merchants),
			(SELECT COUNT(*) FROM tasks),
			(SELECT COUNT(*) FROM tasks WHERE DATE(created_at) = CURDATE()),
			(SELECT COUNT(*) FROM tasks WHERE status IN ('pending','processing')),
			(SELECT COUNT(*) FROM tasks WHERE status = 'failed'),
			(SELECT COUNT(*) FROM tasks WHERE status = 'completed'),
			(SELECT COALESCE(SUM(-points), 0) FROM point_logs WHERE type = 1),
			(SELECT COALESCE(SUM(-points), 0) FROM point_logs WHERE type = 1 AND DATE(created_at) = CURDATE()),
			(SELECT COALESCE(SUM(points), 0) FROM point_logs WHERE type = 4),
			(SELECT COALESCE(SUM(points), 0) FROM point_logs WHERE type IN (2, 3) AND points > 0)`)

	if err := row.Scan(&o.MerchantTotal, &o.MerchantActive, &o.KeyTotal, &o.PointsRemaining,
		&o.TaskTotal, &o.TaskToday, &o.TaskRunning, &o.TaskFailed, &o.TaskCompleted,
		&o.ConsumedTotal, &o.ConsumedToday, &o.RefundedTotal, &o.RechargedTotal); err != nil {
		return nil, err
	}
	return &o, nil
}

// Trend 返回最近 days 天的任务量与消耗，缺失的日期补零。
func (s *Store) Trend(ctx context.Context, days int) ([]model.TrendPoint, error) {
	if days < 1 || days > 90 {
		days = 7
	}

	taskRows, err := s.db.QueryContext(ctx, `
		SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS d,
		       COUNT(*),
		       SUM(status = 'completed'),
		       SUM(status = 'failed')
		FROM tasks
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		GROUP BY d`, days-1)
	if err != nil {
		return nil, err
	}
	defer taskRows.Close()

	type bucket struct{ total, completed, failed, consumed int64 }
	buckets := make(map[string]*bucket)

	for taskRows.Next() {
		var (
			d                        string
			total, completed, failed sql.NullInt64
		)
		if err := taskRows.Scan(&d, &total, &completed, &failed); err != nil {
			return nil, err
		}
		buckets[d] = &bucket{total: total.Int64, completed: completed.Int64, failed: failed.Int64}
	}
	if err := taskRows.Err(); err != nil {
		return nil, err
	}

	pointRows, err := s.db.QueryContext(ctx, `
		SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS d, COALESCE(SUM(-points), 0)
		FROM point_logs
		WHERE type = 1 AND created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		GROUP BY d`, days-1)
	if err != nil {
		return nil, err
	}
	defer pointRows.Close()

	for pointRows.Next() {
		var (
			d        string
			consumed int64
		)
		if err := pointRows.Scan(&d, &consumed); err != nil {
			return nil, err
		}
		if b, ok := buckets[d]; ok {
			b.consumed = consumed
		} else {
			buckets[d] = &bucket{consumed: consumed}
		}
	}
	if err := pointRows.Err(); err != nil {
		return nil, err
	}

	points := make([]model.TrendPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		p := model.TrendPoint{Date: date}
		if b, ok := buckets[date]; ok {
			p.Total, p.Completed, p.Failed, p.Consumed = b.total, b.completed, b.failed, b.consumed
		}
		points = append(points, p)
	}
	return points, nil
}

// KindStats 返回最近 days 天各任务类型的调用量与消耗。
func (s *Store) KindStats(ctx context.Context, days int) ([]model.KindStat, error) {
	if days < 1 || days > 365 {
		days = 30
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, COUNT(*), COALESCE(SUM(points_cost), 0)
		FROM tasks
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		GROUP BY kind
		ORDER BY COUNT(*) DESC`, days-1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]model.KindStat, 0, 12)
	for rows.Next() {
		var s model.KindStat
		if err := rows.Scan(&s.Kind, &s.Count, &s.Consumed); err != nil {
			return nil, err
		}
		s.Label = model.LabelOf(model.TaskKind(s.Kind))
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// MerchantOptions 返回商户下拉选项。
func (s *Store) MerchantOptions(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM merchants ORDER BY id DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	options := make([]map[string]interface{}, 0, 50)
	for rows.Next() {
		var (
			id   int64
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		options = append(options, map[string]interface{}{"id": id, "name": name})
	}
	return options, rows.Err()
}

// confirmAffected 判断 UPDATE 是否真的落到了一行上。
// MySQL 默认返回"实际改变的行数"，新值与旧值相同时为 0，
// 因此不能只凭 RowsAffected 断定记录不存在，需要再确认一次。
func confirmAffected(ctx context.Context, db *sql.DB, res sql.Result, table string, id int64) error {
	if affected, err := res.RowsAffected(); err == nil && affected > 0 {
		return nil
	}

	var exists int
	err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT 1 FROM %s WHERE id = ?`, table), id).Scan(&exists)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	return err
}

func normalizePaging(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	return page, size
}

// DeleteSong 删除一首作品及其衍生任务（MV、格式下载、Remaster、裁剪等），
// 连同其中保存的音频、封面与视频地址一并清除。积分流水保留，便于对账。
func (s *Store) DeleteSong(ctx context.Context, taskID int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var merchantID int64
	var customID sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT merchant_id, custom_id FROM tasks WHERE id = ? AND kind <> 'video' FOR UPDATE`,
		taskID).Scan(&merchantID, &customID); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, taskID)
	if err != nil {
		return 0, err
	}
	deleted, _ := res.RowsAffected()

	if customID.Valid && customID.String != "" {
		// 衍生任务以 suno_id 或 clip_id 引用原作品
		res, err = tx.ExecContext(ctx, `
			DELETE FROM tasks
			WHERE merchant_id = ? AND id <> ?
			  AND (JSON_UNQUOTE(JSON_EXTRACT(request_payload, '$.suno_id')) = ?
			    OR JSON_UNQUOTE(JSON_EXTRACT(request_payload, '$.clip_id')) = ?)`,
			merchantID, taskID, customID.String, customID.String)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		deleted += n
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

/* ---------------------------------- 音色库 ---------------------------------- */

// ListVoices 列出音色库，merchantID 为 0 时返回全部商户。
// 音色即「训练音色」任务，名称与模型名取自提交时的请求参数。
func (s *Store) ListVoices(ctx context.Context, merchantID int64) ([]model.Voice, error) {
	where, args := "t.kind = ?", []interface{}{string(model.KindVoiceTrain)}
	if merchantID > 0 {
		where += " AND t.merchant_id = ?"
		args = append(args, merchantID)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.id, t.merchant_id, m.name, t.status, t.error_message, t.created_at, t.finished_at,
		       JSON_UNQUOTE(JSON_EXTRACT(t.request_payload, '$.name')),
		       JSON_UNQUOTE(JSON_EXTRACT(t.request_payload, '$.model_name'))
		FROM tasks t
		JOIN merchants m ON m.id = t.merchant_id
		WHERE %s
		ORDER BY t.id DESC
		LIMIT 500`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]model.Voice, 0)
	for rows.Next() {
		var v model.Voice
		var status string
		var finished sql.NullTime
		var name, modelName sql.NullString
		if err := rows.Scan(&v.TaskID, &v.MerchantID, &v.MerchantName, &status, &v.ErrorMessage,
			&v.CreatedAt, &finished, &name, &modelName); err != nil {
			return nil, err
		}
		v.Status = model.TaskStatus(status)
		v.Name, v.ModelName = name.String, modelName.String
		if finished.Valid {
			v.FinishedAt = &finished.Time
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// DeleteVoice 从音色库移除一个音色。唱歌克隆没有删除模型的接口，腾讯侧的模型仍会保留。
func (s *Store) DeleteVoice(ctx context.Context, taskID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND kind = ?`,
		taskID, string(model.KindVoiceTrain))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AudioInUse 返回是否还有其他未完成的任务引用同一个音频地址。
func (s *Store) AudioInUse(ctx context.Context, audioURL string, exceptTaskID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE id <> ? AND status IN ('pending','processing')
		  AND JSON_UNQUOTE(JSON_EXTRACT(request_payload, '$.audio_url')) = ?`,
		exceptTaskID, audioURL).Scan(&n)
	return n > 0, err
}

// VoiceCoverOf 取出为某首歌自动发起的最近一次音色翻唱任务，没有时返回 ErrNotFound。
func (s *Store) VoiceCoverOf(ctx context.Context, songTaskID int64) (*model.TaskRow, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM tasks
		WHERE kind = ? AND JSON_EXTRACT(request_payload, '$.song_task_id') = ?
		ORDER BY id DESC LIMIT 1`, string(model.KindVoiceCover), songTaskID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	row, _, err := s.AdminTaskByID(ctx, id)
	return row, err
}

// CreateFailedTask 记下一条没能提交出去的任务（不扣费），让界面能看到失败原因。
func (s *Store) CreateFailedTask(ctx context.Context, t *model.Task, reason string) (int64, error) {
	id, err := s.CreateTask(ctx, t)
	if err != nil {
		return 0, err
	}
	return id, s.FailTask(ctx, id, reason)
}
