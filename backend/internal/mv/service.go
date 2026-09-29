package mv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/media"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/provider"
	"github.com/lepro/suno-open-api/internal/storage"
)

// Options 长 MV 的运行参数。
type Options struct {
	Pricing       Pricing
	MaxDuration   float64 // 允许制作的最长歌曲（秒）
	SeedanceModel string  // Seedance 模型名
	ImageKind     model.TaskKind
	Font          string        // 歌词字幕字体文件，为空时不支持字幕
	Concurrency   int           // 每个项目同时生成的片段数
	Retries       int           // 单段最多尝试次数
	Timeout       time.Duration // 从开始生成到出片的最长时间
	FFmpeg        string        // ffmpeg 可执行文件
	PollInterval  time.Duration
	// 成片存放：COS 存储桶根地址与签名器
	OutputBase string
	Signer     *provider.COSSigner
}

// Service 长 MV 业务：建草稿、改分镜、开始生成、查询。
type Service struct {
	store    *storage.Store
	provider provider.Provider
	writer   Writer
	opts     Options
}

// NewService 创建服务；writer 为 nil 时使用模板写分镜。
func NewService(store *storage.Store, p provider.Provider, writer Writer, opts Options) *Service {
	if writer == nil {
		writer = &TemplateWriter{}
	}
	if opts.Concurrency < 1 {
		opts.Concurrency = 4
	}
	if opts.Retries < 1 {
		opts.Retries = 3
	}
	return &Service{store: store, provider: p, writer: writer, opts: opts}
}

var validTiers = map[string]bool{model.MVTierEconomy: true, model.MVTierStandard: true, model.MVTierPremium: true}

// QuoteRequest 报价参数。
type QuoteRequest struct {
	MerchantID  int64
	SongTaskID  int64
	Tier        string
	ReuseChorus bool
	Resolution  string
	// WithRefs 设置了参考图，视频镜头需要先画首帧
	WithRefs bool
}

// Quote 按与正式生成相同的切分规则算出镜头构成与固定价，供开始前展示。
func (s *Service) Quote(ctx context.Context, req QuoteRequest) (*Quote, error) {
	if !validTiers[req.Tier] {
		return nil, httpx.BadRequest("tier 仅支持 economy、standard 或 premium")
	}
	song, err := s.loadSong(ctx, req.SongTaskID, req.MerchantID)
	if err != nil {
		return nil, err
	}
	segs := Plan(song.duration, song.info.Lyrics, PlanOptions{Tier: req.Tier, ReuseChorus: req.ReuseChorus})
	q := s.opts.Pricing.Quote(segs, req.Resolution, req.WithRefs)
	return &q, nil
}

// SubtitlesReady 返回是否配置了字幕字体。
func (s *Service) SubtitlesReady() bool { return s.opts.Font != "" }

// MaxDuration 返回允许的最长歌曲时长。
func (s *Service) MaxDuration() float64 { return s.opts.MaxDuration }

// Ready 返回成片存储是否可用；未配置 COS 时无法保存成片。
func (s *Service) Ready() bool { return s.opts.Signer.Enabled() && s.opts.OutputBase != "" }

// DraftRequest 新建 MV 的参数。
type DraftRequest struct {
	MerchantID  int64
	SongTaskID  int64
	Mode        string
	Tier        string
	ReuseChorus bool
	Subtitles   bool
	StyleNote   string
	Ratio       string
	Resolution  string
	UseCover    bool
	Look        model.MVLook
	CreatedBy   string
}

// Draft 读取歌曲、切分镜、写提示词并保存为草稿；AI 一键模式下随即开始生成。
func (s *Service) Draft(ctx context.Context, req DraftRequest) (*model.MVProject, error) {
	if !s.Ready() {
		return nil, httpx.BadRequest("未配置 TME_COS_* 存储桶与密钥，无法保存 MV 成片")
	}
	if req.Mode != model.MVModeAuto && req.Mode != model.MVModeManual {
		return nil, httpx.BadRequest("mode 仅支持 auto 或 manual")
	}
	if !validTiers[req.Tier] {
		return nil, httpx.BadRequest("tier 仅支持 economy、standard 或 premium")
	}
	if req.Subtitles && !s.SubtitlesReady() {
		return nil, httpx.BadRequest("未配置字幕字体 MV_FONT_FILE，无法添加歌词字幕")
	}

	look, err := NormalizeLook(req.MerchantID, req.Look)
	if err != nil {
		return nil, err
	}

	song, err := s.loadSong(ctx, req.SongTaskID, req.MerchantID)
	if err != nil {
		return nil, err
	}
	if song.duration > s.opts.MaxDuration {
		return nil, httpx.BadRequest(fmt.Sprintf("歌曲时长 %.0f 秒，超过上限 %.0f 秒", song.duration, s.opts.MaxDuration))
	}

	segs := Plan(song.duration, song.info.Lyrics, PlanOptions{Tier: req.Tier, ReuseChorus: req.ReuseChorus})
	song.info.StyleNote = strings.TrimSpace(req.StyleNote)
	song.info.Ratio = req.Ratio
	song.info.Look = look
	sb := s.write(ctx, song.info, segs)
	for _, seg := range segs {
		seg.Prompt = sb.Prompts[seg.Seq]
	}

	p := &model.MVProject{
		MerchantID:  req.MerchantID,
		SongTaskID:  req.SongTaskID,
		Title:       song.info.Title,
		Mode:        req.Mode,
		Tier:        req.Tier,
		ReuseChorus: req.ReuseChorus,
		Subtitles:   req.Subtitles,
		StyleNote:   song.info.StyleNote,
		VisualBible: sb.VisualBible,
		Look:        look,
		Writer:      sb.Writer,
		Ratio:       req.Ratio,
		Resolution:  req.Resolution,
		UseCover:    req.UseCover && song.cover != "",
		CoverURL:    song.cover,
		AudioURL:    song.audio,
		Duration:    song.duration,
		CreatedBy:   req.CreatedBy,
		// 报价在建草稿时定下，开始生成时按此扣费
		PointsCost: s.opts.Pricing.Quote(segs, req.Resolution, look.HasRefs()).Price,
	}
	id, err := s.store.CreateMVProject(ctx, p, segs)
	if err != nil {
		return nil, err
	}

	if req.Mode == model.MVModeAuto {
		if _, err := s.Start(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

// write 写分镜；大模型失败时退回模板，保证总能出草稿。
func (s *Service) write(ctx context.Context, song SongInfo, segs []*model.MVSegment) *Storyboard {
	wctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	sb, err := s.writer.Write(wctx, song, segs)
	if err == nil {
		return sb
	}
	log.Printf("[mv] 写分镜失败，改用模板：%v", err)
	sb, _ = (&TemplateWriter{}).Write(ctx, song, segs)
	return sb
}

// Rewrite 让 AI 按新的画面要求重新写一遍草稿的分镜；look 不为 nil 时同时更新形象设定并重新报价。
func (s *Service) Rewrite(ctx context.Context, id int64, styleNote string, look *model.MVLook) (*model.MVProject, error) {
	p, err := s.draftOf(ctx, id)
	if err != nil {
		return nil, err
	}
	song, err := s.loadSong(ctx, p.SongTaskID, p.MerchantID)
	if err != nil {
		return nil, err
	}
	segs, err := s.store.MVSegments(ctx, id)
	if err != nil {
		return nil, err
	}

	song.info.StyleNote = strings.TrimSpace(styleNote)
	song.info.Ratio = p.Ratio
	if look != nil {
		l, err := NormalizeLook(p.MerchantID, *look)
		if err != nil {
			return nil, err
		}
		p.Look = l
		p.PointsCost = s.opts.Pricing.Quote(segs, p.Resolution, l.HasRefs()).Price
	}
	song.info.Look = p.Look
	sb := s.write(ctx, song.info, segs)
	prompts := make(map[int64]string, len(segs))
	for _, seg := range segs {
		prompts[seg.ID] = sb.Prompts[seg.Seq]
	}
	if err := s.store.UpdateMVLook(ctx, id, song.info.StyleNote, p.Look, p.PointsCost); err != nil {
		return nil, err
	}
	if err := s.store.UpdateMVStoryboard(ctx, id, sb.VisualBible, sb.Writer, prompts); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// SaveStoryboard 保存运营修改后的整体设定与分镜提示词。
func (s *Service) SaveStoryboard(ctx context.Context, id int64, bible string, prompts map[int64]string) (*model.MVProject, error) {
	if _, err := s.draftOf(ctx, id); err != nil {
		return nil, err
	}
	// 复用镜头沿用被复用镜头的画面，没有自己的提示词
	segs, err := s.store.MVSegments(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, seg := range segs {
		if seg.Kind == model.ShotReuse {
			delete(prompts, seg.ID)
		}
	}
	for segID, prompt := range prompts {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return nil, httpx.BadRequest("分镜提示词不能为空")
		}
		if len([]rune(prompt)) > 800 {
			return nil, httpx.BadRequest("单段提示词最多 800 字")
		}
		prompts[segID] = prompt
	}
	if err := s.store.UpdateMVStoryboard(ctx, id, strings.TrimSpace(bible), "", prompts); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Start 扣除固定价并开始逐段生成。
func (s *Service) Start(ctx context.Context, id int64) (int64, error) {
	p, err := s.draftOf(ctx, id)
	if err != nil {
		return 0, err
	}
	price := int64(0) // 旧草稿也按当前免费政策执行。
	balance, err := s.store.StartMVProject(ctx, id, p.MerchantID, price, "生成长 MV："+p.Title)
	if err != nil {
		if errors.Is(err, storage.ErrInsufficientPoints) {
			return balance, httpx.NoPoints(fmt.Sprintf("积分不足，生成 MV 需要 %d 积分", price))
		}
		return 0, err
	}
	return balance, nil
}

// Retry 重试失败的 MV：已完成的镜头保留，只重做失败部分与合成。失败不退积分，重试免费。
// 返回扣费后的余额与本次扣除的积分。
func (s *Service) Retry(ctx context.Context, id int64) (int64, int64, error) {
	p, err := s.store.MVProjectByID(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return 0, 0, httpx.NotFound("MV 不存在")
		}
		return 0, 0, err
	}
	if p.Status != model.MVFailed {
		return 0, 0, httpx.BadRequest("只有失败的 MV 可以重试")
	}
	// 失败不退积分，重试一律免费
	const cost = 0
	balance, err := s.store.RetryMVProject(ctx, id, p.MerchantID, cost, "重试长 MV："+p.Title)
	if err != nil {
		if errors.Is(err, storage.ErrInsufficientPoints) {
			return balance, 0, httpx.NoPoints(fmt.Sprintf("积分不足，重试 MV 需要 %d 积分", p.PointsCost))
		}
		return 0, 0, err
	}
	return balance, cost, nil
}

// freshAudio 合成前重新挑选音轨：建草稿时记下的地址可能已被上游清理。
func (s *Service) freshAudio(ctx context.Context, p *model.MVProject) string {
	if t, _, err := s.store.AdminTaskByID(ctx, p.SongTaskID); err == nil {
		if u := media.FirstPlayable(ctx, append(media.AudioCandidates(&t.Task), p.AudioURL)); u != "" {
			return u
		}
	}
	return p.AudioURL
}

// Get 查询项目详情，含片段与成片的临时播放地址。
func (s *Service) Get(ctx context.Context, id int64) (*model.MVProject, error) {
	p, err := s.store.MVProjectByID(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("MV 不存在")
		}
		return nil, err
	}
	if p.Segments, err = s.store.MVSegments(ctx, id); err != nil {
		return nil, err
	}
	s.fill(p)
	return p, nil
}

// List 分页列出项目。
func (s *Service) List(ctx context.Context, merchantID int64, page, size int) ([]*model.MVProject, int64, error) {
	list, total, err := s.store.ListMVProjects(ctx, merchantID, page, size)
	for _, p := range list {
		s.fill(p)
	}
	return list, total, err
}

// Delete 删除项目及其成片文件；生成中的项目不能删。
func (s *Service) Delete(ctx context.Context, id int64) error {
	p, err := s.store.MVProjectByID(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("MV 不存在")
		}
		return err
	}
	if p.Status == model.MVGenerating || p.Status == model.MVComposing {
		return httpx.BadRequest("MV 正在生成，完成或失败后才能删除")
	}
	if p.VideoKey != "" && s.Ready() {
		if err := s.opts.Signer.Delete(ctx, &http.Client{Timeout: 30 * time.Second}, s.objectURL(p.VideoKey)); err != nil {
			log.Printf("[mv] 删除成片失败 project=%d: %v", id, err)
		}
	}
	return s.store.DeleteMVProject(ctx, id)
}

// fill 补充展示字段：报价、参考图预览，以及按需签名的成片地址（播放地址始终有效）。
func (s *Service) fill(p *model.MVProject) {
	p.Quote = 0 // 展示当前免费价格，历史积分仍保存在 PointsCost。
	p.Look.RefURLs = s.RefURLs(p.Look)
	if p.VideoKey == "" || !s.Ready() {
		return
	}
	p.VideoURL = s.signKey(p.VideoKey, 24*time.Hour)
}

/* ---------------------------------- 参考图 ---------------------------------- */

const maxRefSize = 10 << 20

var refTypes = map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}

// RefType 返回参考图扩展名对应的类型，不支持的格式返回空串。
func RefType(ext string) string { return refTypes[strings.ToLower(ext)] }

// SaveRef 把参考图存进 COS，返回对象路径与临时预览地址。
func (s *Service) SaveRef(ctx context.Context, merchantID int64, ext string, data []byte) (string, string, error) {
	if !s.Ready() {
		return "", "", httpx.BadRequest("未配置 TME_COS_* 存储桶与密钥，无法保存参考图")
	}
	ext = strings.ToLower(ext)
	contentType := RefType(ext)
	if contentType == "" {
		return "", "", httpx.BadRequest("参考图仅支持 jpg、png、webp 格式")
	}
	if len(data) == 0 || len(data) > maxRefSize {
		return "", "", httpx.BadRequest("参考图为空或超过 10MB")
	}
	now := time.Now()
	key := fmt.Sprintf("%s%s/%d%s", refPrefix(merchantID), now.Format("20060102"), now.UnixNano(), ext)
	if err := s.opts.Signer.Put(ctx, &http.Client{Timeout: 2 * time.Minute}, s.objectURL(key), contentType, data); err != nil {
		return "", "", httpx.Internal("保存参考图失败：" + err.Error())
	}
	return key, s.signKey(key, 24*time.Hour), nil
}

// ImportRef 下载上游生成的图片（如定妆照）转存为参考图：上游地址约一天后失效。
func (s *Service) ImportRef(ctx context.Context, merchantID int64, src string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return "", "", httpx.Internal("下载图片失败：" + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", httpx.Internal(fmt.Sprintf("下载图片失败：HTTP %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRefSize+1))
	if err != nil {
		return "", "", err
	}
	ext := urlExt(src, "")
	if RefType(ext) == "" {
		ext = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[resp.Header.Get("Content-Type")]
	}
	if ext == "" {
		ext = ".png"
	}
	return s.SaveRef(ctx, merchantID, ext, data)
}

// RefURLs 把参考图路径签成上游与浏览器都能访问的临时地址。
func (s *Service) RefURLs(l model.MVLook) []string {
	if !s.Ready() || len(l.RefKeys) == 0 {
		return nil
	}
	urls := make([]string, 0, len(l.RefKeys))
	for _, k := range l.RefKeys {
		if u := s.signKey(k, 24*time.Hour); u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}

// PortraitPayload 生成定妆照的绘画参数；有参考图时一并带上，让定妆照贴近运营给的照片。
func (s *Service) PortraitPayload(merchantID int64, look model.MVLook, styleNote string) (model.TaskKind, map[string]interface{}, error) {
	l, err := NormalizeLook(merchantID, look)
	if err != nil {
		return "", nil, err
	}
	if len([]rune(styleNote)) > 500 {
		return "", nil, httpx.BadRequest("画面要求最多 500 字")
	}
	payload := map[string]interface{}{"prompt": PortraitPrompt(l, styleNote), "aspect_ratio": "3:4", "image_size": "1K"}
	if refs := s.RefURLs(l); len(refs) > 0 {
		payload["reference_images"] = refs
	}
	return s.opts.ImageKind, payload, nil
}

func (s *Service) signKey(key string, expire time.Duration) string {
	signer := *s.opts.Signer
	signer.Expire = expire
	u, err := signer.Sign(s.objectURL(key), time.Now())
	if err != nil {
		return ""
	}
	return u
}

func (s *Service) objectURL(key string) string {
	return strings.TrimRight(s.opts.OutputBase, "/") + "/" + key
}

func (s *Service) draftOf(ctx context.Context, id int64) (*model.MVProject, error) {
	p, err := s.store.MVProjectByID(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("MV 不存在")
		}
		return nil, err
	}
	if p.Status != model.MVDraft {
		return nil, httpx.BadRequest("该 MV 已开始生成，不能再修改")
	}
	return p, nil
}

/* ---------------------------------- 读取歌曲 ---------------------------------- */

type songSource struct {
	info     SongInfo
	audio    string
	cover    string
	duration float64
}

// loadSong 从作品任务里取出歌名、风格、歌词、音频、封面与时长。
func (s *Service) loadSong(ctx context.Context, taskID, merchantID int64) (*songSource, error) {
	t, raw, err := s.store.AdminTaskByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("作品不存在")
		}
		return nil, err
	}
	if t.MerchantID != merchantID {
		return nil, httpx.BadRequest("该作品不属于所选商户")
	}
	if t.Status != model.StatusCompleted {
		return nil, httpx.BadRequest("作品尚未生成完成")
	}

	var req struct {
		Title        string `json:"title"`
		Tags         string `json:"tags"`
		Prompt       string `json:"prompt"`
		Description  string `json:"gpt_description_prompt"`
		Instrumental bool   `json:"make_instrumental"`
	}
	_ = json.Unmarshal([]byte(raw), &req)

	song := &songSource{info: SongInfo{
		Title: req.Title, Tags: req.Tags, Lyrics: req.Prompt, Instrumental: req.Instrumental,
	}}
	if f := t.FileInfo; f != nil {
		song.cover = f.CoverURL
		song.duration = f.Duration
	}

	// 上游完整数据里有 AI 实际写出的歌词、风格和时长，灵感模式的歌词只在这里
	var clips []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		ImageURL string `json:"image_url"`
		AudioURL string `json:"audio_url"`
		Metadata struct {
			Tags     string  `json:"tags"`
			Prompt   string  `json:"prompt"`
			Duration float64 `json:"duration"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(t.Extend), &clips) == nil {
		for _, c := range clips {
			if t.CustomID != nil && c.ID != *t.CustomID && len(clips) > 1 {
				continue
			}
			song.info.Title = firstNonEmpty(song.info.Title, c.Title)
			song.info.Tags = firstNonEmpty(c.Metadata.Tags, song.info.Tags)
			song.info.Lyrics = firstNonEmpty(c.Metadata.Prompt, song.info.Lyrics)
			song.cover = firstNonEmpty(song.cover, c.ImageURL)
			if song.duration <= 0 {
				song.duration = c.Metadata.Duration
			}
			break
		}
	}

	if song.info.Title == "" {
		song.info.Title = "未命名作品"
	}
	if song.info.Lyrics == "" && !song.info.Instrumental {
		song.info.Lyrics = req.Description
	}
	// 上游转存的音频约一天后失效，逐个探测候选地址，取当前能访问的
	if song.audio = media.PlayableAudio(ctx, &t.Task); song.audio == "" {
		return nil, httpx.BadRequest("该作品的音频已失效，无法制作 MV")
	}
	if song.duration <= 0 {
		return nil, httpx.BadRequest("无法获取歌曲时长，暂不能制作 MV")
	}
	return song, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
