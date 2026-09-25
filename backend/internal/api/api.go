// Package api 暴露对外的 HTTP 接口，路径与文档保持一致。
package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lepro/suno-open-api/internal/certificate"
	"github.com/lepro/suno-open-api/internal/config"
	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/middleware"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/musicreq"
	"github.com/lepro/suno-open-api/internal/storage"
	"github.com/lepro/suno-open-api/internal/task"
)

// Server 聚合处理接口所需的依赖。
type Server struct {
	cfg   *config.Config
	store *storage.Store
	tasks *task.Service
	certs *certificate.Service
}

// NewServer 创建接口服务。
func NewServer(cfg *config.Config, store *storage.Store, tasks *task.Service) *Server {
	return &Server{cfg: cfg, store: store, tasks: tasks}
}

// Routes 注册全部路由。registrars 用于挂载运营后台等额外路由。
func (s *Server) Routes(registrars ...func(*httpx.Router)) http.Handler {
	r := httpx.NewRouter()
	r.Use(middleware.Recover(), middleware.Logger(), middleware.CORS(s.cfg.AllowedOrigins))

	auth := middleware.Auth(s.store)
	limit := middleware.RateLimit(s.cfg.RateLimitPerMinute)

	// 健康检查，不需要鉴权
	r.GET("/healthz", func(w http.ResponseWriter, r *http.Request) error {
		httpx.JSON(w, map[string]interface{}{"status": "ok", "provider": s.cfg.Provider})
		return nil
	})

	// 系统通用
	r.GET("/api/v1/points/balance", s.balance, auth, limit)
	r.GET("/api/v1/points/logs", s.pointLogs, auth, limit)

	// 音乐生成
	r.POST("/api/v1/music/generate", s.generate, auth, limit)
	r.POST("/api/v1/music/sound", s.sound, auth, limit)
	r.GET("/api/v1/music/task", s.queryTask, auth, limit)
	r.GET("/api/v1/music/tasks", s.queryTasks, auth, limit)
	r.POST("/api/v1/music/retry", s.retryTask, auth, limit)
	r.POST("/api/v1/music/upload", s.upload, auth, limit)
	r.POST("/api/v1/music/whole-song", s.wholeSong, auth, limit)
	r.POST("/api/v1/music/aligned-lyrics", s.alignedLyrics, auth, limit)
	r.POST("/api/v1/music/upsample", s.upsample, auth, limit)
	r.POST("/api/v1/music/video", s.video, auth, limit)
	r.POST("/api/v1/music/crop", s.crop, auth, limit)
	r.POST("/api/v1/music/speed", s.speed, auth, limit)

	// 格式下载（v2）
	r.POST("/api/v2/music/download-wav", s.download(model.KindDownloadWAV), auth, limit)
	r.POST("/api/v2/music/download-mp3", s.download(model.KindDownloadMP3), auth, limit)
	r.POST("/api/v2/music/download-m4a", s.download(model.KindDownloadM4A), auth, limit)

	// 创作证明
	s.registerCertificates(r, auth, limit)

	for _, register := range registrars {
		register(r)
	}

	return r
}

/* ---------------------------------- 系统通用 ---------------------------------- */

func (s *Server) balance(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}
	points, err := s.store.Balance(r.Context(), merchant.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"remaining_points": points})
	return nil
}

func (s *Server) pointLogs(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}
	page := httpx.QueryInt(r, "page", 1)
	limit := httpx.QueryInt(r, "limit", 20)

	logs, total, err := s.store.PointLogs(r.Context(), merchant.ID, page, limit)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"total": total, "page": page, "limit": limit, "list": logs})
	return nil
}

/* ---------------------------------- 生成类 ---------------------------------- */

func (s *Server) generate(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req musicreq.Generate
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}

	payload, err := req.Payload()
	if err != nil {
		return err
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindGenerate, payload, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids)
	return nil
}

type soundRequest struct {
	Title string   `json:"title"`
	Tags  string   `json:"tags"`
	MV    string   `json:"mv"`
	Tempo *float64 `json:"tempo"`
	Key   string   `json:"key"`
	Loop  *bool    `json:"loop"`
}

func (s *Server) sound(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req soundRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}

	if strings.TrimSpace(req.Title) == "" {
		return httpx.BadRequest("缺少必填参数 title")
	}
	if len([]rune(req.Title)) > 100 {
		return httpx.BadRequest("title 最多 100 字符")
	}
	if strings.TrimSpace(req.Tags) == "" {
		return httpx.BadRequest("缺少必填参数 tags")
	}
	if len([]rune(req.Tags)) > 1000 {
		return httpx.BadRequest("tags 最多 1000 字符")
	}
	mv, ok := model.NormalizeModel(model.UsageSound, req.MV)
	if !ok {
		return httpx.BadRequest("mv 不是受支持的音效模型：" + strings.Join(model.ModelCodes(model.UsageSound), "、"))
	}

	// 音效生成的固定参数由服务端补齐，调用方无需关心
	payload := map[string]interface{}{
		"title":             req.Title,
		"tags":              req.Tags,
		"mv":                mv,
		"task":              "sound",
		"generation_type":   "TEXT",
		"make_instrumental": true,
	}
	soundConfigs := map[string]interface{}{}
	if req.Tempo != nil {
		soundConfigs["tempo"] = *req.Tempo
	}
	if req.Key != "" {
		soundConfigs["key"] = req.Key
	}
	if req.Loop != nil {
		soundConfigs["loop"] = *req.Loop
	}
	if len(soundConfigs) > 0 {
		payload["metadata"] = map[string]interface{}{"sound_configs": soundConfigs}
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindSound, payload, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"task_ids": toStrings(ids)})
	return nil
}

/* ---------------------------------- 查询类 ---------------------------------- */

func (s *Server) queryTask(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}
	id, err := httpx.QueryInt64(r, "id")
	if err != nil {
		return err
	}
	t, err := s.tasks.Get(r.Context(), merchant.ID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, t)
	return nil
}

type retryRequest struct {
	ID int64 `json:"id"`
}

// retryTask 免费重试失败的任务：任务失败不退积分，可用原参数重新生成，任务 ID 不变。
func (s *Server) retryTask(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}
	var req retryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少参数 id")
	}
	t, err := s.tasks.Retry(r.Context(), merchant.ID, req.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, t)
	return nil
}

func (s *Server) queryTasks(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	raw := strings.TrimSpace(r.URL.Query().Get("ids"))
	if raw == "" {
		return httpx.BadRequest("缺少参数 ids")
	}

	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, convErr := strconv.ParseInt(p, 10, 64)
		if convErr != nil {
			return httpx.BadRequest("ids 只能是用逗号分隔的数字任务 ID")
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return httpx.BadRequest("ids 不能为空")
	}
	if len(ids) > 100 {
		return httpx.BadRequest("一次最多查询 100 个任务")
	}

	page := httpx.QueryInt(r, "page", 1)
	size := httpx.QueryInt(r, "size", 10)

	tasks, total, err := s.tasks.List(r.Context(), merchant.ID, ids, page, size)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"total": total, "page": page, "size": size, "list": tasks})
	return nil
}

/* ---------------------------------- 加工类 ---------------------------------- */

type uploadRequest struct {
	AudioURL       string `json:"audio_url"`
	CopyrightAudio bool   `json:"copyrightAudio"`
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req uploadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if !strings.HasPrefix(req.AudioURL, "http://") && !strings.HasPrefix(req.AudioURL, "https://") {
		return httpx.BadRequest("audio_url 必须是可公开访问的 http/https 地址")
	}

	var surcharge int64
	if req.CopyrightAudio {
		surcharge = model.CopyrightAudioSurcharge
	}

	payload := map[string]interface{}{"audio_url": req.AudioURL, "copyrightAudio": req.CopyrightAudio}
	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindUpload, payload, surcharge)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids[0])
	return nil
}

type clipRequest struct {
	ClipID string `json:"clip_id"`
}

func (s *Server) wholeSong(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req clipRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := requireClipID(req.ClipID, "clip_id"); err != nil {
		return err
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindWholeSong,
		map[string]interface{}{"clip_id": req.ClipID}, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids[0])
	return nil
}

type alignedLyricsRequest struct {
	Lyrics string `json:"lyrics"`
	SunoID string `json:"suno_id"`
}

func (s *Server) alignedLyrics(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req alignedLyricsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Lyrics) == "" {
		return httpx.BadRequest("缺少必填参数 lyrics")
	}
	if err := requireClipID(req.SunoID, "suno_id"); err != nil {
		return err
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindAlignedLyrics,
		map[string]interface{}{"lyrics": req.Lyrics, "suno_id": req.SunoID}, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids[0])
	return nil
}

type upsampleRequest struct {
	ClipID            string `json:"clip_id"`
	ModelName         string `json:"model_name"`
	VariationCategory string `json:"variation_category"`
}

var variationCategories = []string{"subtle", "normal", "high"}

func (s *Server) upsample(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req upsampleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := requireClipID(req.ClipID, "clip_id"); err != nil {
		return err
	}
	modelName, ok := model.NormalizeModel(model.UsageRemaster, req.ModelName)
	if !ok {
		return httpx.BadRequest("model_name 不是受支持的 Remaster 模型：" + strings.Join(model.ModelCodes(model.UsageRemaster), "、"))
	}
	if req.VariationCategory != "" && !contains(variationCategories, req.VariationCategory) {
		return httpx.BadRequest("variation_category 仅支持 subtle、normal 或 high")
	}

	payload := map[string]interface{}{"clip_id": req.ClipID, "model_name": modelName}
	putIfNotEmpty(payload, "variation_category", req.VariationCategory)

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindUpsample, payload, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids)
	return nil
}

type videoRequest struct {
	TaskID *int64 `json:"task_id"`
	SunoID string `json:"suno_id"`
}

func (s *Server) video(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req videoRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.TaskID == nil || *req.TaskID <= 0 {
		return httpx.BadRequest("缺少必填参数 task_id")
	}
	if err := requireClipID(req.SunoID, "suno_id"); err != nil {
		return err
	}
	// 校验 task_id 属于当前商户，避免为别人的任务生成视频
	source, err := s.tasks.Get(r.Context(), merchant.ID, *req.TaskID)
	if err != nil {
		return err
	}

	// 上游要的是它自己的任务 ID，不是本平台的编号，这里做一次转换
	upstreamTaskID := source.ProviderTaskID
	if upstreamTaskID == "" {
		return httpx.BadRequest("该任务尚未提交到上游，暂时无法生成视频")
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindVideo,
		map[string]interface{}{"task_id": upstreamTaskID, "suno_id": req.SunoID}, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids[0])
	return nil
}

type cropRequest struct {
	ClipID     string   `json:"clip_id"`
	CropStartS *float64 `json:"crop_start_s"`
	CropEndS   *float64 `json:"crop_end_s"`
}

func (s *Server) crop(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req cropRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := requireClipID(req.ClipID, "clip_id"); err != nil {
		return err
	}
	if req.CropStartS == nil || req.CropEndS == nil {
		return httpx.BadRequest("crop_start_s 与 crop_end_s 均为必填")
	}
	if *req.CropStartS < 0 {
		return httpx.BadRequest("crop_start_s 必须大于等于 0")
	}
	if *req.CropEndS <= *req.CropStartS {
		return httpx.BadRequest("crop_end_s 必须大于 crop_start_s")
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindCrop, map[string]interface{}{
		"clip_id":      req.ClipID,
		"crop_start_s": *req.CropStartS,
		"crop_end_s":   *req.CropEndS,
	}, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids[0])
	return nil
}

type speedRequest struct {
	ClipID          string   `json:"clip_id"`
	SpeedMultiplier *float64 `json:"speed_multiplier"`
	KeepPitch       bool     `json:"keep_pitch"`
	Title           string   `json:"title"`
}

var speedOptions = []float64{0.25, 0.5, 0.75, 1, 1.25, 1.5, 2}

func (s *Server) speed(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}

	var req speedRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := requireClipID(req.ClipID, "clip_id"); err != nil {
		return err
	}
	if req.SpeedMultiplier == nil {
		return httpx.BadRequest("缺少必填参数 speed_multiplier")
	}

	valid := false
	for _, opt := range speedOptions {
		if *req.SpeedMultiplier == opt {
			valid = true
			break
		}
	}
	if !valid {
		return httpx.BadRequest("speed_multiplier 仅支持 0.25、0.5、0.75、1、1.25、1.5、2")
	}

	payload := map[string]interface{}{
		"clip_id":          req.ClipID,
		"speed_multiplier": *req.SpeedMultiplier,
		"keep_pitch":       req.KeepPitch,
	}
	putIfNotEmpty(payload, "title", req.Title)

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindSpeed, payload, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, ids[0])
	return nil
}

type downloadRequest struct {
	SunoID string `json:"suno_id"`
}

// download 返回三个格式转换接口共用的处理函数。
func (s *Server) download(kind model.TaskKind) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		merchant, err := middleware.MerchantFrom(r)
		if err != nil {
			return err
		}

		var req downloadRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}
		if err := requireClipID(req.SunoID, "suno_id"); err != nil {
			return err
		}

		ids, err := s.tasks.Submit(r.Context(), merchant.ID, kind,
			map[string]interface{}{"suno_id": req.SunoID}, 0)
		if err != nil {
			return err
		}
		httpx.JSON(w, map[string]interface{}{
			"task_id": strconv.FormatInt(ids[0], 10),
			"status":  string(model.StatusPending),
		})
		return nil
	}
}

/* ---------------------------------- 校验辅助 ---------------------------------- */

// requireClipID 校验 Suno 音乐 ID：不能为空，且不能误传纯数字的 task_id。
func requireClipID(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return httpx.BadRequest("缺少必填参数 " + field)
	}
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return httpx.BadRequest(field + " 需要传 Suno 音乐 ID（custom_id），不是数字 task_id")
	}
	return nil
}

func contains(options []string, value string) bool {
	for _, opt := range options {
		if opt == value {
			return true
		}
	}
	return false
}

func putIfNotEmpty(m map[string]interface{}, key, value string) {
	if strings.TrimSpace(value) != "" {
		m[key] = value
	}
}

func toStrings(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strconv.FormatInt(id, 10))
	}
	return out
}
