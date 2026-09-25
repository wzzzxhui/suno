// Package upstream 监控上游账户余额。
//
// 上游没有提供 webhook，无法在充值后主动通知我们，
// 因此这里定时调用它的余额接口（该接口免费）并缓存结果，
// 效果上等同于"充值后自动更新"，延迟不超过一个轮询周期。
package upstream

import (
	"context"
	"log"
	"sync"
	"time"
)

// BalanceReader 能查询自身账户余额的 Provider。
type BalanceReader interface {
	Balance(ctx context.Context) (int64, error)
}

// Snapshot 一次余额查询的结果快照。
type Snapshot struct {
	// Supported 为 false 表示当前 Provider 不支持查询（如本地 mock）
	Supported bool       `json:"supported"`
	Balance   int64      `json:"balance"`
	Low       bool       `json:"low"`
	Threshold int64      `json:"threshold"`
	CheckedAt *time.Time `json:"checked_at"`
	Error     string     `json:"error,omitempty"`
}

// Monitor 周期性拉取上游余额。
type Monitor struct {
	reader    BalanceReader
	interval  time.Duration
	threshold int64

	mu       sync.RWMutex
	snapshot Snapshot
}

// NewMonitor 创建监控器。reader 为 nil 表示当前 Provider 不支持余额查询。
func NewMonitor(reader BalanceReader, interval time.Duration, threshold int64) *Monitor {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Monitor{
		reader:    reader,
		interval:  interval,
		threshold: threshold,
		snapshot:  Snapshot{Supported: reader != nil, Threshold: threshold},
	}
}

// Run 阻塞运行，直到 ctx 结束。
func (m *Monitor) Run(ctx context.Context) {
	if m.reader == nil {
		return
	}

	m.refresh(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	log.Printf("[upstream] 余额监控启动，间隔 %s，低额阈值 %d", m.interval, m.threshold)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refresh(ctx)
		}
	}
}

// Refresh 立即查询一次，供后台"手动刷新"使用。
func (m *Monitor) Refresh(ctx context.Context) Snapshot {
	m.refresh(ctx)
	return m.Snapshot()
}

func (m *Monitor) refresh(ctx context.Context) {
	if m.reader == nil {
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	balance, err := m.reader.Balance(queryCtx)
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.snapshot.Supported = true
	m.snapshot.Threshold = m.threshold
	m.snapshot.CheckedAt = &now

	if err != nil {
		// 查询失败时保留上一次的余额数值，只记录错误，避免界面数字来回跳
		m.snapshot.Error = err.Error()
		log.Printf("[upstream] 查询余额失败：%v", err)
		return
	}

	if m.snapshot.Error != "" || m.snapshot.Balance != balance {
		log.Printf("[upstream] 余额更新：%d", balance)
	}
	m.snapshot.Error = ""
	m.snapshot.Balance = balance
	m.snapshot.Low = m.threshold > 0 && balance < m.threshold
}

// Snapshot 返回最近一次查询结果。
func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}
