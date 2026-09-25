// Package storage 负责 MySQL 持久化：商户、密钥、任务与积分流水。
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/lepro/suno-open-api/internal/model"
)

// ErrNotFound 查询不到记录。
var ErrNotFound = errors.New("记录不存在")

// ErrInsufficientPoints 积分不足。
var ErrInsufficientPoints = errors.New("积分不足")

// Store 封装数据库访问。
type Store struct {
	db *sql.DB
}

// Open 连接 MySQL 并校验连通性。
func Open(dsn string, maxOpen, maxIdle int, lifetime time.Duration) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return &Store{db: db}, nil
}

// DB 暴露底层连接，便于迁移脚本使用。
func (s *Store) DB() *sql.DB { return s.db }

// Close 关闭连接。
func (s *Store) Close() error { return s.db.Close() }

/* ---------------------------------- 商户与密钥 ---------------------------------- */

// CreateMerchant 新建商户并赠送初始积分。
func (s *Store) CreateMerchant(ctx context.Context, name string, bonus int64) (*model.Merchant, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO merchants (name, status, points) VALUES (?, 1, ?)`, name, bonus)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if bonus > 0 {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO point_logs (merchant_id, type, points, balance, remark) VALUES (?, ?, ?, ?, ?)`,
			id, model.PointRecharge, bonus, bonus, "新用户注册赠送"); err != nil {
			return nil, err
		}
	}
	return s.MerchantByID(ctx, id)
}

// MerchantByID 按主键查询商户。
func (s *Store) MerchantByID(ctx context.Context, id int64) (*model.Merchant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, status, points, created_at, updated_at FROM merchants WHERE id = ?`, id)

	var m model.Merchant
	if err := row.Scan(&m.ID, &m.Name, &m.Status, &m.Points, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

// CreateAPIKey 保存密钥哈希。
func (s *Store) CreateAPIKey(ctx context.Context, merchantID int64, name, prefix, hash string) (*model.APIKey, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (merchant_id, name, key_prefix, key_hash, status) VALUES (?, ?, ?, ?, 1)`,
		merchantID, name, prefix, hash)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.APIKey{ID: id, MerchantID: merchantID, Name: name, Prefix: prefix, Hash: hash, Status: 1}, nil
}

// MerchantByKeyHash 通过密钥哈希定位商户，同时校验启用状态。
func (s *Store) MerchantByKeyHash(ctx context.Context, hash string) (*model.Merchant, int64, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT m.id, m.name, m.status, m.points, m.created_at, m.updated_at, k.id
		FROM api_keys k
		JOIN merchants m ON m.id = k.merchant_id
		WHERE k.key_hash = ? AND k.status = 1`, hash)

	var m model.Merchant
	var keyID int64
	if err := row.Scan(&m.ID, &m.Name, &m.Status, &m.Points, &m.CreatedAt, &m.UpdatedAt, &keyID); err != nil {
		if err == sql.ErrNoRows {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	return &m, keyID, nil
}

// TouchAPIKey 记录密钥最近使用时间，失败不影响主流程。
func (s *Store) TouchAPIKey(ctx context.Context, keyID int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = ?`, keyID)
}

/* ---------------------------------- 积分 ---------------------------------- */

// Balance 查询余额。
func (s *Store) Balance(ctx context.Context, merchantID int64) (int64, error) {
	var points int64
	err := s.db.QueryRowContext(ctx, `SELECT points FROM merchants WHERE id = ?`, merchantID).Scan(&points)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return points, err
}

// Deduct 在事务中扣减积分并写入流水，余额不足时返回 ErrInsufficientPoints。
func (s *Store) Deduct(ctx context.Context, merchantID, amount int64, remark string) (int64, error) {
	if amount <= 0 {
		return s.Balance(ctx, merchantID)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT points FROM merchants WHERE id = ? FOR UPDATE`, merchantID).Scan(&balance); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}

	if balance < amount {
		return balance, ErrInsufficientPoints
	}

	balance -= amount
	if _, err := tx.ExecContext(ctx, `UPDATE merchants SET points = ? WHERE id = ?`, balance, merchantID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO point_logs (merchant_id, type, points, balance, remark) VALUES (?, ?, ?, ?, ?)`,
		merchantID, model.PointConsume, -amount, balance, remark); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return balance, nil
}

// Refund 退还任务积分，并把任务标记为已退款。同一任务只会退一次。
func (s *Store) Refund(ctx context.Context, taskID int64, remark string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var merchantID, cost int64
	var refunded bool
	if err := tx.QueryRowContext(ctx,
		`SELECT merchant_id, points_cost, points_refunded FROM tasks WHERE id = ? FOR UPDATE`,
		taskID).Scan(&merchantID, &cost, &refunded); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
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
		`INSERT INTO point_logs (merchant_id, type, points, balance, remark, task_id) VALUES (?, ?, ?, ?, ?, ?)`,
		merchantID, model.PointRefund, cost, balance, remark, taskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET points_refunded = 1 WHERE id = ?`, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// Recharge 充值或手动调整积分。
func (s *Store) Recharge(ctx context.Context, merchantID, amount int64, logType int, remark string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var balance int64
	if err := tx.QueryRowContext(ctx,
		`SELECT points FROM merchants WHERE id = ? FOR UPDATE`, merchantID).Scan(&balance); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}

	balance += amount
	if balance < 0 {
		return 0, ErrInsufficientPoints
	}
	if _, err := tx.ExecContext(ctx, `UPDATE merchants SET points = ? WHERE id = ?`, balance, merchantID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO point_logs (merchant_id, type, points, balance, remark) VALUES (?, ?, ?, ?, ?)`,
		merchantID, logType, amount, balance, remark); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return balance, nil
}

// PointLogs 分页查询积分流水。
func (s *Store) PointLogs(ctx context.Context, merchantID int64, page, limit int) ([]model.PointLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM point_logs WHERE merchant_id = ?`, merchantID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, points, balance, remark, task_id, created_at
		FROM point_logs WHERE merchant_id = ?
		ORDER BY id DESC LIMIT ? OFFSET ?`, merchantID, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	logs := make([]model.PointLog, 0, limit)
	for rows.Next() {
		var l model.PointLog
		if err := rows.Scan(&l.ID, &l.Type, &l.Points, &l.Balance, &l.Remark, &l.TaskID, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		l.MerchantID = merchantID
		logs = append(logs, l)
	}
	return logs, total, rows.Err()
}

/* ---------------------------------- 任务 ---------------------------------- */

// CreateTask 写入一条新任务。
func (s *Store) CreateTask(ctx context.Context, t *model.Task) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (merchant_id, kind, status, provider_task_id, request_payload, extra_param, points_cost)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.MerchantID, string(t.Kind), string(model.StatusPending), t.ProviderTaskID,
		nullString(t.Request), t.ExtraParam, t.PointsCost)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetProviderTaskID 提交到上游后回填上游任务 ID。
func (s *Store) SetProviderTaskID(ctx context.Context, taskID int64, providerID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET provider_task_id = ?, status = ? WHERE id = ?`,
		providerID, string(model.StatusProcessing), taskID)
	return err
}

// TaskByID 查询单个任务（带商户校验）。
func (s *Store) TaskByID(ctx context.Context, merchantID, taskID int64) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, merchant_id, kind, status, provider_task_id, custom_id, proxy_url, file_info,
		       extend, extra_param, error_message, points_cost, points_refunded,
		       created_at, updated_at, finished_at, retry_count, retried_at
		FROM tasks WHERE id = ? AND merchant_id = ?`, taskID, merchantID)
	return scanTask(row)
}

// TasksByIDs 批量查询任务。
func (s *Store) TasksByIDs(ctx context.Context, merchantID int64, ids []int64, page, size int) ([]*model.Task, int64, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids)+3)
	args = append(args, merchantID)
	for _, id := range ids {
		args = append(args, id)
	}

	var total int64
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM tasks WHERE merchant_id = ? AND id IN (%s)`, placeholders)
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listSQL := fmt.Sprintf(`
		SELECT id, merchant_id, kind, status, provider_task_id, custom_id, proxy_url, file_info,
		       extend, extra_param, error_message, points_cost, points_refunded,
		       created_at, updated_at, finished_at, retry_count, retried_at
		FROM tasks WHERE merchant_id = ? AND id IN (%s)
		ORDER BY id ASC LIMIT ? OFFSET ?`, placeholders)
	args = append(args, size, (page-1)*size)

	rows, err := s.db.QueryContext(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tasks := make([]*model.Task, 0, len(ids))
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}
	return tasks, total, rows.Err()
}

// PendingTasks 取出仍未进入终态的任务，供轮询 worker 使用。
func (s *Store) PendingTasks(ctx context.Context, limit int) ([]*model.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, merchant_id, kind, status, provider_task_id, custom_id, proxy_url, file_info,
		       extend, extra_param, error_message, points_cost, points_refunded,
		       created_at, updated_at, finished_at, retry_count, retried_at
		FROM tasks
		WHERE status IN ('pending','processing')
		ORDER BY updated_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]*model.Task, 0, limit)
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// CompleteTask 写入任务成功结果。
func (s *Store) CompleteTask(ctx context.Context, taskID int64, customID, proxyURL, extend string, info *model.FileInfo) error {
	var infoJSON interface{}
	if info != nil {
		raw, err := json.Marshal(info)
		if err != nil {
			return err
		}
		infoJSON = string(raw)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, custom_id = ?, proxy_url = ?, file_info = ?, extend = ?, finished_at = NOW()
		WHERE id = ?`,
		string(model.StatusCompleted), nullString(customID), proxyURL, infoJSON, nullString(extend), taskID)
	return err
}

// FailTask 标记任务失败。
func (s *Store) FailTask(ctx context.Context, taskID int64, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, error_message = ?, finished_at = NOW() WHERE id = ?`,
		string(model.StatusFailed), reason, taskID)
	return err
}

// RetryTask 让失败的任务用新的上游任务号重新开始，重试次数加一；超过上限或状态不对时返回 ErrNotFound。
func (s *Store) RetryTask(ctx context.Context, taskID int64, providerID string, maxRetries int) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET status = ?, provider_task_id = ?, error_message = '', finished_at = NULL,
		       retry_count = retry_count + 1, retried_at = NOW()
		WHERE id = ? AND status = ? AND retry_count < ?`,
		string(model.StatusPending), providerID, taskID, string(model.StatusFailed), maxRetries)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkProcessing 把任务推进到处理中。
func (s *Store) MarkProcessing(ctx context.Context, taskID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status = ? WHERE id = ? AND status = ?`,
		string(model.StatusProcessing), taskID, string(model.StatusPending))
	return err
}

// Touch 更新 updated_at，避免 worker 反复抢同一批任务。
func (s *Store) Touch(ctx context.Context, taskID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET updated_at = NOW() WHERE id = ?`, taskID)
	return err
}

/* ---------------------------------- 内部辅助 ---------------------------------- */

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanTask(row scanner) (*model.Task, error) {
	t, err := scanTaskRows(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func scanTaskRows(row scanner) (*model.Task, error) {
	var (
		t        model.Task
		kind     string
		status   string
		customID sql.NullString
		fileInfo sql.NullString
		extend   sql.NullString
		finished sql.NullTime
		retried  sql.NullTime
	)

	err := row.Scan(&t.ID, &t.MerchantID, &kind, &status, &t.ProviderTaskID, &customID, &t.ProxyURL,
		&fileInfo, &extend, &t.ExtraParam, &t.ErrorMessage, &t.PointsCost, &t.PointsRefunded,
		&t.CreatedAt, &t.UpdatedAt, &finished, &t.RetryCount, &retried)
	if err != nil {
		return nil, err
	}

	t.Kind = model.TaskKind(kind)
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
	if retried.Valid {
		r := retried.Time
		t.RetriedAt = &r
	}
	return &t, nil
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
