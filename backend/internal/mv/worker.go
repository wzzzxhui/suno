package mv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/provider"
)

// Worker 推进生成中的项目：逐段提交 Seedance、轮询结果，全部出片后合成成片。
type Worker struct {
	svc *Service

	mu        sync.Mutex
	composing map[int64]bool
}

// NewWorker 创建 worker。
func NewWorker(svc *Service) *Worker {
	return &Worker{svc: svc, composing: make(map[int64]bool)}
}

// Run 阻塞运行，直到 ctx 结束。
func (w *Worker) Run(ctx context.Context) {
	interval := w.svc.opts.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	projects, err := w.svc.store.MVProjectsByStatus(ctx, model.MVGenerating, model.MVComposing)
	if err != nil {
		log.Printf("[mv] 拉取项目失败: %v", err)
		return
	}
	for _, p := range projects {
		switch p.Status {
		case model.MVGenerating:
			w.advance(ctx, p)
		case model.MVComposing:
			// 服务重启后遗留的合成中项目在这里重新合成
			w.startCompose(ctx, p)
		}
	}
}

// advance 同步生成中的片段，并按并发上限补交待生成的片段。
func (w *Worker) advance(ctx context.Context, p *model.MVProject) {
	opts := w.svc.opts
	if p.StartedAt != nil && opts.Timeout > 0 && time.Since(*p.StartedAt) > opts.Timeout {
		w.fail(ctx, p, "生成超时")
		return
	}

	segs, err := w.svc.store.MVSegments(ctx, p.ID)
	if err != nil || len(segs) == 0 {
		return
	}

	running, done := 0, 0
	for _, seg := range segs {
		if seg.Status != model.SegRunning {
			continue
		}
		res, err := w.svc.provider.Fetch(ctx, &provider.FetchRequest{Kind: w.kindOf(p, seg), ProviderID: seg.ProviderTaskID})
		if err != nil {
			running++ // 查询失败下轮再试
			continue
		}
		switch res.Status {
		case model.StatusCompleted:
			keyframe := w.drawingKeyframe(p, seg)
			url := mediaURL(seg, keyframe, res)
			if url == "" {
				if !w.retrySegment(ctx, p, seg, "片段完成但没有素材地址") {
					return
				}
				continue
			}
			if keyframe {
				// 首帧画好后回到待生成，下一轮以首帧为起点提交视频
				seg.Status, seg.KeyframeURL, seg.ProviderTaskID, seg.Attempts, seg.ErrorMessage =
					model.SegPending, url, "", 0, ""
				_ = w.svc.store.UpdateMVSegment(ctx, seg)
				continue
			}
			seg.Status, seg.VideoURL, seg.ErrorMessage = model.SegCompleted, url, ""
			_ = w.svc.store.UpdateMVSegment(ctx, seg)
		case model.StatusFailed:
			if !w.retrySegment(ctx, p, seg, firstNonEmpty(res.Reason, "片段生成失败")) {
				return
			}
		default:
			running++
		}
	}

	var refs []string // 参考图按需签名，一轮只签一次
	for _, seg := range segs {
		switch seg.Status {
		case model.SegCompleted:
			done++
		case model.SegPending:
			if running >= opts.Concurrency {
				continue
			}
			if refs == nil {
				refs = w.svc.RefURLs(p.Look)
				if p.Look.HasRefs() && len(refs) == 0 {
					w.fail(ctx, p, "参考图无法访问，请检查 COS 配置")
					return
				}
			}
			if w.submit(ctx, p, seg, refs) {
				running++
			}
		}
	}

	if done == len(segs) {
		if err := w.svc.store.SetMVStatus(ctx, p.ID, model.MVComposing, "", ""); err != nil {
			return
		}
		p.Status = model.MVComposing
		w.startCompose(ctx, p)
	}
}

// drawingKeyframe 视频镜头是否处在画首帧阶段：设置了参考图时，先按参考图画首帧再生成视频，
// 否则 Seedance 纯文生视频每段的人物都会不同。
func (w *Worker) drawingKeyframe(p *model.MVProject, seg *model.MVSegment) bool {
	return seg.Kind == model.ShotVideo && p.Look.HasRefs() && seg.KeyframeURL == ""
}

// kindOf 返回镜头当前阶段的生成能力：图片镜头与视频首帧走绘画模型，视频走 Seedance。
func (w *Worker) kindOf(p *model.MVProject, seg *model.MVSegment) model.TaskKind {
	if seg.Kind == model.ShotImage || w.drawingKeyframe(p, seg) {
		return w.svc.opts.ImageKind
	}
	return model.KindVideoSeedance
}

// mediaURL 取出镜头产出的素材地址；keyframe 为 true 时取首帧图片。
func mediaURL(seg *model.MVSegment, keyframe bool, res *provider.FetchResult) string {
	if seg.Kind == model.ShotImage || keyframe {
		if res.FileInfo != nil && len(res.FileInfo.Images) > 0 {
			return res.FileInfo.Images[0]
		}
		return ""
	}
	if res.FileInfo != nil && res.FileInfo.MP4URL != "" {
		return res.FileInfo.MP4URL
	}
	return res.ProxyURL
}

// SubmitPayload 组装镜头当前阶段提交上游的参数；refs 为已签名的参考图地址。
func SubmitPayload(p *model.MVProject, seg *model.MVSegment, refs []string, seedanceModel string) map[string]interface{} {
	// 没有人物参考图时，首镜沿用封面作参考
	if len(refs) == 0 && seg.Seq == 0 && p.UseCover && p.CoverURL != "" {
		refs = []string{p.CoverURL}
	}

	keyframe := seg.Kind == model.ShotVideo && p.Look.HasRefs() && seg.KeyframeURL == ""
	if seg.Kind == model.ShotImage || keyframe {
		// 图片与成片同比例，1080p 成片用 2K 图，保证运镜放大后仍清晰
		size := "1K"
		if p.Resolution == "1080p" {
			size = "2K"
		}
		prompt := ComposeImagePrompt(p.VisualBible, seg.Prompt, p.Look)
		if keyframe {
			prompt += "\n这张图是一段视频的第一帧，人物姿态为动作的起始状态。"
		}
		payload := map[string]interface{}{"prompt": prompt, "aspect_ratio": p.Ratio, "image_size": size}
		if len(refs) > 0 {
			payload["reference_images"] = refs
		}
		return payload
	}

	payload := map[string]interface{}{
		"model":      seedanceModel,
		"ratio":      p.Ratio,
		"resolution": p.Resolution,
		"duration":   ClipSeconds(seg),
	}
	if seg.KeyframeURL != "" {
		// 以按参考图画出的首帧为起点，人物外貌已确定，提示词只写动作与运镜
		payload["prompt"] = ComposeVideoPrompt(seg.Prompt, p.Look)
		payload["reference_images"] = []string{seg.KeyframeURL}
		return payload
	}
	payload["prompt"] = ComposeImagePrompt(p.VisualBible, seg.Prompt, p.Look)
	if len(refs) > 0 {
		payload["reference_images"] = refs[:1] // Seedance 把参考图当首帧，只取一张
	}
	return payload
}

func (w *Worker) submit(ctx context.Context, p *model.MVProject, seg *model.MVSegment, refs []string) bool {
	payload := SubmitPayload(p, seg, refs, w.svc.opts.SeedanceModel)
	res, err := w.svc.provider.Submit(ctx, &provider.SubmitRequest{Kind: w.kindOf(p, seg), Payload: payload})
	if err != nil || len(res.ProviderIDs) == 0 {
		// 提交失败多半是上游暂时不可用，不计入重试次数，下轮再交
		seg.ErrorMessage = fmt.Sprintf("提交失败，稍后重试：%v", err)
		_ = w.svc.store.UpdateMVSegment(ctx, seg)
		return false
	}
	seg.Status, seg.ProviderTaskID, seg.ErrorMessage = model.SegRunning, res.ProviderIDs[0], ""
	seg.Attempts++
	_ = w.svc.store.UpdateMVSegment(ctx, seg)
	return true
}

// retrySegment 片段失败时重置为待生成；超过次数则整条 MV 判失败，返回是否继续推进。
func (w *Worker) retrySegment(ctx context.Context, p *model.MVProject, seg *model.MVSegment, reason string) bool {
	if seg.Attempts >= w.svc.opts.Retries {
		seg.Status, seg.ErrorMessage = model.SegFailed, reason
		_ = w.svc.store.UpdateMVSegment(ctx, seg)
		w.fail(ctx, p, fmt.Sprintf("第 %d 段重试 %d 次仍失败：%s", seg.Seq+1, seg.Attempts, reason))
		return false
	}
	seg.Status, seg.ProviderTaskID, seg.ErrorMessage = model.SegPending, "", reason
	_ = w.svc.store.UpdateMVSegment(ctx, seg)
	return true
}

func (w *Worker) startCompose(ctx context.Context, p *model.MVProject) {
	w.mu.Lock()
	if w.composing[p.ID] {
		w.mu.Unlock()
		return
	}
	w.composing[p.ID] = true
	w.mu.Unlock()

	go func() {
		defer func() {
			w.mu.Lock()
			delete(w.composing, p.ID)
			w.mu.Unlock()
		}()
		cctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()

		key, err := w.compose(cctx, p)
		var gone *mediaGone
		if errors.As(err, &gone) {
			// 素材过期只补拍对应镜头，不判整条失败
			log.Printf("[mv] 第 %d 段素材失效，重新生成 project=%d", gone.seq+1, p.ID)
			if rerr := w.svc.store.ResetMVSegments(ctx, p.ID, []int{gone.seq}, "素材链接已过期，重新生成"); rerr != nil {
				w.fail(ctx, p, rerr.Error())
			}
			return
		}
		if err != nil {
			log.Printf("[mv] 合成失败 project=%d: %v", p.ID, err)
			w.fail(ctx, p, err.Error())
			return
		}
		if err := w.svc.store.SetMVStatus(ctx, p.ID, model.MVCompleted, key, ""); err != nil {
			log.Printf("[mv] 写入成片失败 project=%d: %v", p.ID, err)
			return
		}
		log.Printf("[mv] 成片完成 project=%d %s", p.ID, key)
	}()
}

// compose 拼接全部片段并上传 COS，返回成片路径。
func (w *Worker) compose(ctx context.Context, p *model.MVProject) (string, error) {
	segs, err := w.svc.store.MVSegments(ctx, p.ID)
	if err != nil {
		return "", err
	}
	bySeq := make(map[int]*model.MVSegment, len(segs))
	for _, seg := range segs {
		bySeq[seg.Seq] = seg
	}
	clips := make([]Clip, len(segs))
	subs := make([]SubtitleSource, len(segs))
	sourceSeq := make([]int, len(segs)) // 每个镜头实际取素材的镜头序号
	for i, seg := range segs {
		// 复用镜头取被复用镜头的素材，时长仍按自己的
		src := seg
		if seg.Kind == model.ShotReuse && seg.ReuseOf != nil {
			if s, ok := bySeq[*seg.ReuseOf]; ok {
				src = s
			}
		}
		if src.VideoURL == "" {
			return "", fmt.Errorf("第 %d 段缺少素材", seg.Seq+1)
		}
		clips[i] = Clip{URL: src.VideoURL, Image: src.Kind == model.ShotImage, Length: seg.Length(), Motion: seg.Seq}
		sourceSeq[i] = src.Seq
		subs[i] = SubtitleSource{Start: seg.Start, End: seg.End, Lyrics: seg.Lyrics}
	}

	out := filepath.Join(os.TempDir(), fmt.Sprintf("mv_%d_%d.mp4", p.ID, time.Now().UnixNano()))
	defer os.Remove(out)

	in := ComposeInput{FFmpeg: w.svc.opts.FFmpeg, Clips: clips, AudioURL: w.svc.freshAudio(ctx, p), Duration: p.Duration, Out: out}
	in.Width, in.Height = Canvas(p.Ratio, p.Resolution)
	if p.Subtitles && w.svc.opts.Font != "" {
		in.Subtitles, in.Font = BuildSubtitles(subs), w.svc.opts.Font
	}
	if err := Compose(ctx, in); err != nil {
		var mg *MediaGoneError
		if errors.As(err, &mg) {
			return "", &mediaGone{seq: sourceSeq[mg.Clip]}
		}
		return "", err
	}

	data, err := os.ReadFile(out)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("mv/m%d/%d_%d.mp4", p.MerchantID, p.ID, time.Now().Unix())
	client := &http.Client{Timeout: 10 * time.Minute}
	if err := w.svc.opts.Signer.Put(ctx, client, w.svc.objectURL(key), "video/mp4", data); err != nil {
		return "", err
	}
	return key, nil
}

// mediaGone 合成时发现某个镜头素材失效，seq 为需要重新生成的镜头。
type mediaGone struct{ seq int }

func (e *mediaGone) Error() string { return fmt.Sprintf("第 %d 段素材失效", e.seq+1) }

// fail 标记失败。已生成的镜头都消耗了上游成本，失败不退积分，由运营免费重试。
func (w *Worker) fail(ctx context.Context, p *model.MVProject, reason string) {
	if err := w.svc.store.SetMVStatus(ctx, p.ID, model.MVFailed, "", reason); err != nil {
		log.Printf("[mv] 标记失败出错 project=%d: %v", p.ID, err)
	}
}
