// Package task 负责任务编排：扣积分、提交上游、落库、轮询回填与失败退款。
package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lepro/suno-open-api/internal/config"
	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/media"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/provider"
	"github.com/lepro/suno-open-api/internal/storage"
)

// Service 组合存储与上游 Provider。
type Service struct {
	store    *storage.Store
	provider provider.Provider
	cfg      *config.Config
	cleaner  *SampleCleaner
	archive  func(ctx context.Context, taskID int64)
	// coverSource 给音色翻唱准备原曲地址（转成 MP3 放到 COS）；为 nil 时直接用作品原始音频
	coverSource func(ctx context.Context, t *model.Task) (string, error)
}

// SetCoverSource 设置音色翻唱的原曲地址生成方式。
func (s *Service) SetCoverSource(fn func(ctx context.Context, t *model.Task) (string, error)) {
	s.coverSource = fn
}

// CoverAudio 为音色翻唱取原曲地址：优先转成 MP3 的 COS 地址，取不到时退回作品当前可播放的音频。
func (s *Service) CoverAudio(ctx context.Context, t *model.Task) string {
	// 首次转 MP3 要下载、转码、上传，可能要二三十秒；不跟随请求取消，避免浏览器超时后白做
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
	defer cancel()
	if s.coverSource != nil {
		if u, err := s.coverSource(ctx, t); err == nil && u != "" {
			return u
		} else if err != nil {
			log.Printf("[voice] 原曲转 MP3 失败 task=%d，改用原始音频: %v", t.ID, err)
		}
	}
	return media.PlayableAudio(ctx, t)
}

// SetArchiver 设置任务完成后的转存动作，为 nil 时不转存。
func (s *Service) SetArchiver(fn func(ctx context.Context, taskID int64)) { s.archive = fn }

// SetCleaner 设置任务结束后的音频清理器，为 nil 时不清理。
func (s *Service) SetCleaner(c *SampleCleaner) { s.cleaner = c }

// New 创建任务服务。
func New(store *storage.Store, p provider.Provider, cfg *config.Config) *Service {
	return &Service{store: store, provider: p, cfg: cfg}
}

// Submit 提交一次任务：先扣积分，再发往上游，最后落库。
// surcharge 用于「受版权保护音频」这类额外扣费。
func (s *Service) Submit(ctx context.Context, merchantID int64, kind model.TaskKind, payload map[string]interface{}, surcharge int64) ([]int64, error) {
	cost := model.PriceOf(kind) + surcharge

	if cost > 0 {
		if _, err := s.store.Deduct(ctx, merchantID, cost, remarkOf(kind)); err != nil {
			if errors.Is(err, storage.ErrInsufficientPoints) {
				return nil, httpx.NoPoints("积分不足，请先充值")
			}
			return nil, err
		}
	}

	result, err := s.provider.Submit(ctx, &provider.SubmitRequest{Kind: kind, Payload: payload})
	if err != nil {
		// 上游没收下任务，立即把积分退回去
		if cost > 0 {
			if _, refundErr := s.store.Recharge(ctx, merchantID, cost, model.PointRefund, "提交失败退还："+remarkOf(kind)); refundErr != nil {
				log.Printf("[task] 退款失败 merchant=%d cost=%d: %v", merchantID, cost, refundErr)
			}
		}
		log.Printf("[task] 提交上游失败 kind=%s: %v", kind, err)
		return nil, httpx.Internal("提交上游失败：" + err.Error())
	}

	raw, _ := json.Marshal(payload)
	costs := splitCost(cost, len(result.ProviderIDs))

	ids := make([]int64, 0, len(result.ProviderIDs))
	for i, providerID := range result.ProviderIDs {
		t := &model.Task{
			MerchantID:     merchantID,
			Kind:           kind,
			ProviderTaskID: providerID,
			ExtraParam:     remarkOf(kind),
			PointsCost:     costs[i],
			Request:        string(raw),
		}
		id, err := s.store.CreateTask(ctx, t)
		if err != nil {
			return nil, err
		}
		if err := s.store.SetProviderTaskID(ctx, id, providerID); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Retry 免费重试失败的任务：用原请求参数重新提交上游，任务编号不变。
// merchantID 大于 0 时校验任务归属（对外接口），为 0 时不校验（运营后台）。
func (s *Service) Retry(ctx context.Context, merchantID, taskID int64) (*model.Task, error) {
	row, raw, err := s.store.AdminTaskByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("任务不存在")
		}
		return nil, err
	}
	t := &row.Task
	if merchantID > 0 && t.MerchantID != merchantID {
		return nil, httpx.NotFound("任务不存在")
	}
	if t.Status != model.StatusFailed {
		return nil, httpx.BadRequest("只有失败的任务可以重试")
	}
	max := s.cfg.TaskMaxRetries
	if t.RetryCount >= max {
		return nil, httpx.BadRequest(fmt.Sprintf("该任务已重试 %d 次，不能再重试", max))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload == nil {
		return nil, httpx.BadRequest("任务缺少原始参数，无法重试")
	}
	// 翻唱作品库里的歌：原曲地址会过期，且旧任务可能用的是 Opus 原文件，重试时重新生成
	if t.Kind == model.KindVoiceCover {
		if songID, ok := payload["song_task_id"].(float64); ok && songID > 0 {
			if song, _, err := s.store.AdminTaskByID(ctx, int64(songID)); err == nil {
				if u := s.CoverAudio(ctx, &song.Task); u != "" {
					payload["audio_url"] = u
					raw, _ := json.Marshal(payload)
					_ = s.store.SetRequestPayload(ctx, taskID, string(raw))
				}
			}
		}
	}

	result, err := s.provider.Submit(ctx, &provider.SubmitRequest{Kind: t.Kind, Payload: payload})
	if err != nil {
		log.Printf("[task] 重试提交上游失败 task=%d: %v", taskID, err)
		return nil, httpx.Internal("重试提交上游失败：" + err.Error())
	}
	// 生成音乐一次产出两个版本，重试只补这一条：Mureka 的结果按「任务号#序号」区分，保持原序号；其余取第一个
	providerID := result.ProviderIDs[0]
	if i := strings.LastIndexByte(t.ProviderTaskID, '#'); i > 0 {
		suffix := t.ProviderTaskID[i:]
		for _, id := range result.ProviderIDs {
			if strings.HasSuffix(id, suffix) {
				providerID = id
			}
		}
	}
	if err := s.store.RetryTask(ctx, taskID, providerID, max); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.BadRequest("任务状态已变化，请刷新后再试")
		}
		return nil, err
	}
	return s.Get(ctx, t.MerchantID, taskID)
}

// Get 查询单个任务。
func (s *Service) Get(ctx context.Context, merchantID, taskID int64) (*model.Task, error) {
	t, err := s.store.TaskByID(ctx, merchantID, taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("任务不存在")
		}
		return nil, err
	}
	return t, nil
}

// List 批量查询任务。
func (s *Service) List(ctx context.Context, merchantID int64, ids []int64, page, size int) ([]*model.Task, int64, error) {
	return s.store.TasksByIDs(ctx, merchantID, ids, page, size)
}

// splitCost 把一次请求的费用平摊到多个产出任务上，余数给第一条，
// 这样单条任务失败时可以按比例退款。
func splitCost(total int64, n int) []int64 {
	if n <= 0 {
		return nil
	}
	costs := make([]int64, n)
	base := total / int64(n)
	for i := range costs {
		costs[i] = base
	}
	costs[0] += total - base*int64(n)
	return costs
}

func remarkOf(kind model.TaskKind) string {
	switch kind {
	case model.KindGenerate:
		return "生成音乐"
	case model.KindSound:
		return "生成音效"
	case model.KindUpload:
		return "上传参考音频"
	case model.KindWholeSong:
		return "获取整首歌"
	case model.KindAlignedLyrics:
		return "获取歌词时间戳"
	case model.KindUpsample:
		return "Remaster 音乐"
	case model.KindVideo:
		return "生成音乐视频"
	case model.KindCrop:
		return "裁剪音乐"
	case model.KindSpeed:
		return "调整音乐速度"
	case model.KindDownloadWAV:
		return "下载 WAV"
	case model.KindDownloadMP3:
		return "下载 MP3"
	case model.KindDownloadM4A:
		return "下载 M4A"
	case model.KindVoiceTrain:
		return "训练音色"
	case model.KindVoiceCover:
		return "音色翻唱"
	default:
		return model.LabelOf(kind)
	}
}

/* ---------------------------------- 轮询 worker ---------------------------------- */

// Worker 周期性地把未完成任务同步到终态。
type Worker struct {
	svc      *Service
	interval time.Duration
	workers  int
	timeout  time.Duration
	// 训练音色耗时远超普通任务，单独计算超时
	trainTimeout time.Duration
}

// NewWorker 创建轮询器。
func NewWorker(svc *Service, cfg *config.Config) *Worker {
	return &Worker{
		svc:      svc,
		interval: cfg.PollInterval,
		workers:  cfg.PollWorkers,
		timeout:  cfg.TaskTimeout,

		trainTimeout: cfg.VoiceTrainTimeout,
	}
}

// Run 阻塞运行，直到 ctx 结束。
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	log.Printf("[worker] 启动，间隔 %s，并发 %d", w.interval, w.workers)
	for {
		select {
		case <-ctx.Done():
			log.Println("[worker] 退出")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	tasks, err := w.svc.store.PendingTasks(ctx, w.workers*10)
	if err != nil {
		log.Printf("[worker] 拉取待处理任务失败: %v", err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	queue := make(chan *model.Task)
	var wg sync.WaitGroup

	for i := 0; i < w.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range queue {
				w.sync(ctx, t)
			}
		}()
	}

	for _, t := range tasks {
		select {
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return
		case queue <- t:
		}
	}
	close(queue)
	wg.Wait()
}

// sync 同步单个任务的状态。
func (w *Worker) sync(ctx context.Context, t *model.Task) {
	// 超时保护：长时间不出结果的任务判失败，可免费重试；重试后从重试时刻重新计时
	timeout := w.timeout
	if t.Kind == model.KindVoiceTrain {
		timeout = w.trainTimeout
	}
	start := t.CreatedAt
	if t.RetriedAt != nil {
		start = *t.RetriedAt
	}
	if timeout > 0 && time.Since(start) > timeout {
		w.fail(ctx, t, "任务超时未完成")
		return
	}
	if t.ProviderTaskID == "" {
		w.fail(ctx, t, "缺少上游任务 ID")
		return
	}

	res, err := w.svc.provider.Fetch(ctx, &provider.FetchRequest{Kind: t.Kind, ProviderID: t.ProviderTaskID})
	if err != nil {
		log.Printf("[worker] 查询上游失败 task=%d: %v", t.ID, err)
		_ = w.svc.store.Touch(ctx, t.ID)
		return
	}

	switch res.Status {
	case model.StatusCompleted:
		if err := w.svc.store.CompleteTask(ctx, t.ID, res.CustomID, res.ProxyURL, res.Extend, res.FileInfo); err != nil {
			log.Printf("[worker] 写入任务结果失败 task=%d: %v", t.ID, err)
			return
		}
		w.svc.cleaner.Clean(ctx, t)
		if w.svc.archive != nil {
			w.svc.archive(ctx, t.ID)
		}
	case model.StatusFailed:
		reason := res.Reason
		if reason == "" {
			reason = "上游返回任务失败"
		}
		w.fail(ctx, t, reason)
	default:
		if t.Status == model.StatusPending {
			_ = w.svc.store.MarkProcessing(ctx, t.ID)
		}
		if res.Progress != nil {
			_ = w.svc.store.SetProgress(ctx, t.ID, res.Progress)
		} else {
			_ = w.svc.store.Touch(ctx, t.ID)
		}
	}
}

// fail 标记失败。生成失败不退积分，由调用方免费重试；
// 上传的素材要留给重试使用，因此失败时不清理。
func (w *Worker) fail(ctx context.Context, t *model.Task, reason string) {
	if err := w.svc.store.FailTask(ctx, t.ID, reason); err != nil {
		log.Printf("[worker] 标记任务失败出错 task=%d: %v", t.ID, err)
	}
}
