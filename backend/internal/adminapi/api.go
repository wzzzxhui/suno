package adminapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/archive"
	"github.com/lepro/suno-open-api/internal/certificate"
	"github.com/lepro/suno-open-api/internal/config"
	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/middleware"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/musicreq"
	"github.com/lepro/suno-open-api/internal/mv"
	"github.com/lepro/suno-open-api/internal/storage"
	"github.com/lepro/suno-open-api/internal/task"
	"github.com/lepro/suno-open-api/internal/upstream"
)

// Server 运营后台接口。
type Server struct {
	cfg     *config.Config
	store   *storage.Store
	tasks   *task.Service
	monitor *upstream.Monitor
	mv      *mv.Service
	archive *archive.Archiver
	certs   *certificate.Service
	mp3     *archive.MP3Maker
	secret  string
	ttl     time.Duration
}

// NewServer 创建运营后台接口服务。
func NewServer(cfg *config.Config, store *storage.Store, tasks *task.Service, monitor *upstream.Monitor, mvSvc *mv.Service) *Server {
	return &Server{
		cfg:     cfg,
		store:   store,
		tasks:   tasks,
		monitor: monitor,
		mv:      mvSvc,
		secret:  cfg.AdminSecret,
		ttl:     cfg.AdminTokenTTL,
	}
}

// Register 把后台路由挂到主路由上。
func (s *Server) Register(r *httpx.Router) {
	auth := func(h httpx.Handler) httpx.Handler { return s.authMiddleware(h) }

	r.POST("/admin/api/login", s.login)

	r.GET("/admin/api/profile", s.profile, auth)
	r.POST("/admin/api/password", s.changePassword, auth)

	r.GET("/admin/api/overview", s.overview, auth)
	r.GET("/admin/api/trend", s.trend, auth)
	r.GET("/admin/api/kind-stats", s.kindStats, auth)
	r.GET("/admin/api/system", s.system, auth)
	r.GET("/admin/api/models", s.models, auth)
	r.GET("/admin/api/upstream", s.upstreamBalance, auth)
	r.POST("/admin/api/upstream/refresh", s.refreshUpstream, auth)

	r.GET("/admin/api/merchants", s.merchants, auth)
	r.GET("/admin/api/merchants/options", s.merchantOptions, auth)
	r.POST("/admin/api/merchants/create", s.createMerchant, auth)
	r.POST("/admin/api/merchants/status", s.merchantStatus, auth)
	r.POST("/admin/api/merchants/rename", s.renameMerchant, auth)

	r.GET("/admin/api/keys", s.keys, auth)
	r.POST("/admin/api/keys/create", s.createKey, auth)
	r.POST("/admin/api/keys/status", s.keyStatus, auth)
	r.POST("/admin/api/keys/delete", s.deleteKey, auth)

	r.POST("/admin/api/music/generate", s.generateMusic, auth)
	r.POST("/admin/api/music/video", s.generateVideo, auth)
	r.POST("/admin/api/music/upload", s.uploadMusic, auth)
	r.POST("/admin/api/songs/delete", s.deleteSong, auth)
	r.GET("/admin/api/songs/mp3", s.songMP3, auth)
	s.registerCertificates(r, auth)

	r.GET("/admin/api/voices", s.voices, auth)
	r.POST("/admin/api/voices/train", s.trainVoice, auth)
	r.POST("/admin/api/voices/delete", s.deleteVoice, auth)
	r.POST("/admin/api/voices/cover", s.coverWithVoice, auth)
	r.POST("/admin/api/voices/sample", s.uploadSample, auth)

	r.GET("/admin/api/mv/projects", s.mvProjects, auth)
	r.GET("/admin/api/mv/project", s.mvProject, auth)
	r.POST("/admin/api/mv/quote", s.mvQuote, auth)
	r.POST("/admin/api/mv/create", s.mvCreate, auth)
	r.POST("/admin/api/mv/rewrite", s.mvRewrite, auth)
	r.POST("/admin/api/mv/storyboard", s.mvSaveStoryboard, auth)
	r.POST("/admin/api/mv/start", s.mvStart, auth)
	r.POST("/admin/api/mv/retry", s.mvRetry, auth)
	r.POST("/admin/api/mv/delete", s.mvDelete, auth)
	r.POST("/admin/api/mv/ref", s.mvUploadRef, auth)
	r.POST("/admin/api/mv/portrait", s.mvPortrait, auth)
	r.GET("/admin/api/mv/portrait", s.mvPortraitStatus, auth)
	r.GET("/admin/api/songs", s.songs, auth)
	r.GET("/admin/api/capabilities", s.capabilities, auth)
	r.POST("/admin/api/studio/generate", s.studioGenerate, auth)

	r.GET("/admin/api/tasks", s.taskList, auth)
	r.GET("/admin/api/tasks/detail", s.taskDetail, auth)
	r.POST("/admin/api/tasks/refund", s.refundTask, auth)
	r.POST("/admin/api/tasks/retry", s.retryTask, auth)

	r.GET("/admin/api/points/logs", s.pointLogs, auth)
	r.POST("/admin/api/points/adjust", s.adjustPoints, auth)
}

/* ---------------------------------- 登录与账号 ---------------------------------- */

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		return httpx.BadRequest("请输入用户名与密码")
	}

	user, err := s.store.AdminByUsername(r.Context(), strings.TrimSpace(req.Username))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// 不区分"用户不存在"与"密码错误"，避免账号枚举
			return httpx.Unauthorized("用户名或密码错误")
		}
		return err
	}
	if !VerifyPassword(req.Password, user.PasswordHash) {
		return httpx.Unauthorized("用户名或密码错误")
	}
	if user.Status != 1 {
		return httpx.Unauthorized("账号已被禁用")
	}

	token, expiresAt, err := signToken(s.secret, user.ID, user.Username, s.ttl)
	if err != nil {
		return err
	}
	s.store.TouchAdminLogin(r.Context(), user.ID)

	httpx.JSON(w, map[string]interface{}{
		"token":      token,
		"expires_at": expiresAt.Format(time.RFC3339),
		"user":       user,
	})
	return nil
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}
	httpx.JSON(w, user)
	return nil
}

type passwordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req passwordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if !VerifyPassword(req.OldPassword, user.PasswordHash) {
		return httpx.BadRequest("原密码不正确")
	}
	if len(req.NewPassword) < 8 {
		return httpx.BadRequest("新密码至少 8 位")
	}

	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	if err := s.store.UpdateAdminPassword(r.Context(), user.ID, hash); err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"updated": true})
	return nil
}

/* ---------------------------------- 统计 ---------------------------------- */

func (s *Server) overview(w http.ResponseWriter, r *http.Request) error {
	data, err := s.store.Overview(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, data)
	return nil
}

func (s *Server) trend(w http.ResponseWriter, r *http.Request) error {
	data, err := s.store.Trend(r.Context(), httpx.QueryInt(r, "days", 7))
	if err != nil {
		return err
	}
	httpx.JSON(w, data)
	return nil
}

func (s *Server) kindStats(w http.ResponseWriter, r *http.Request) error {
	data, err := s.store.KindStats(r.Context(), httpx.QueryInt(r, "days", 30))
	if err != nil {
		return err
	}
	httpx.JSON(w, data)
	return nil
}

// system 返回运行配置与各任务类型定价，供设置页展示。
func (s *Server) system(w http.ResponseWriter, r *http.Request) error {
	prices := make([]map[string]interface{}, 0, 12)
	for _, kind := range model.AllKinds() {
		prices = append(prices, map[string]interface{}{
			"kind":   string(kind),
			"label":  model.LabelOf(kind),
			"points": model.PriceOf(kind),
			"amount": float64(model.PriceOf(kind)) / 100,
		})
	}

	httpx.JSON(w, map[string]interface{}{
		"provider":              s.cfg.Provider,
		"upstream_base":         s.cfg.UpstreamBase,
		"upstream_configured":   s.cfg.UpstreamKey != "",
		"poll_interval":         s.cfg.PollInterval.String(),
		"poll_workers":          s.cfg.PollWorkers,
		"task_timeout":          s.cfg.TaskTimeout.String(),
		"task_max_retries":      s.cfg.TaskMaxRetries,
		"rate_limit_per_minute": s.cfg.RateLimitPerMinute,
		"signup_bonus":          s.cfg.SignupBonus,
		"copyright_surcharge":   model.CopyrightAudioSurcharge,
		"voice_configured":      s.cfg.VoiceConfigured(),
		"prices":                prices,
	})
	return nil
}

// upstreamBalance 返回缓存的上游余额。
// 上游没有 webhook，这里是定时轮询的结果，延迟不超过一个轮询周期。
func (s *Server) upstreamBalance(w http.ResponseWriter, r *http.Request) error {
	snap := s.monitor.Snapshot()
	httpx.JSON(w, map[string]interface{}{
		"snapshot": snap,
		"provider": s.cfg.Provider,
		"interval": s.cfg.UpstreamBalanceInterval.String(),
	})
	return nil
}

// refreshUpstream 立即向上游查一次，用于充值后马上看到结果。
func (s *Server) refreshUpstream(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, map[string]interface{}{"snapshot": s.monitor.Refresh(r.Context())})
	return nil
}

// models 返回三类用途下的全部可选模型，供后台下拉与文档展示。
func (s *Server) models(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, map[string]interface{}{
		"generate": model.ModelsFor(model.UsageGenerate),
		"sound":    model.ModelsFor(model.UsageSound),
		"remaster": model.ModelsFor(model.UsageRemaster),
		"defaults": map[string]string{
			"generate": model.DefaultModel(model.UsageGenerate),
			"sound":    model.DefaultModel(model.UsageSound),
			"remaster": model.DefaultModel(model.UsageRemaster),
		},
	})
	return nil
}

/* ---------------------------------- 商户 ---------------------------------- */

func (s *Server) merchants(w http.ResponseWriter, r *http.Request) error {
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	status := httpx.QueryInt(r, "status", -1)
	page := httpx.QueryInt(r, "page", 1)
	size := httpx.QueryInt(r, "size", 20)

	list, total, err := s.store.ListMerchants(r.Context(), keyword, status, page, size)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"total": total, "page": page, "size": size, "list": list})
	return nil
}

func (s *Server) merchantOptions(w http.ResponseWriter, r *http.Request) error {
	options, err := s.store.MerchantOptions(r.Context())
	if err != nil {
		return err
	}
	httpx.JSON(w, options)
	return nil
}

type createMerchantRequest struct {
	Name      string `json:"name"`
	Points    int64  `json:"points"`
	CreateKey bool   `json:"create_key"`
}

func (s *Server) createMerchant(w http.ResponseWriter, r *http.Request) error {
	var req createMerchantRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return httpx.BadRequest("请填写商户名称")
	}
	if req.Points < 0 {
		return httpx.BadRequest("赠送积分不能为负数")
	}

	bonus := req.Points
	if bonus == 0 {
		bonus = s.cfg.SignupBonus
	}

	merchant, err := s.store.CreateMerchant(r.Context(), req.Name, bonus)
	if err != nil {
		return err
	}

	result := map[string]interface{}{"merchant": merchant}
	if req.CreateKey {
		accessKey, key, err := s.issueKey(r, merchant.ID, "default")
		if err != nil {
			return err
		}
		result["access_key"] = accessKey
		result["key"] = key
	}
	httpx.JSON(w, result)
	return nil
}

type idStatusRequest struct {
	ID     int64 `json:"id"`
	Status int   `json:"status"`
}

func (s *Server) merchantStatus(w http.ResponseWriter, r *http.Request) error {
	var req idStatusRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少商户 ID")
	}
	if req.Status != 0 && req.Status != 1 {
		return httpx.BadRequest("status 只能是 0 或 1")
	}

	if err := s.store.SetMerchantStatus(r.Context(), req.ID, req.Status); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("商户不存在")
		}
		return err
	}
	httpx.JSON(w, map[string]interface{}{"updated": true})
	return nil
}

type renameRequest struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (s *Server) renameMerchant(w http.ResponseWriter, r *http.Request) error {
	var req renameRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.ID <= 0 || req.Name == "" {
		return httpx.BadRequest("缺少商户 ID 或名称")
	}

	if err := s.store.RenameMerchant(r.Context(), req.ID, req.Name); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("商户不存在")
		}
		return err
	}
	httpx.JSON(w, map[string]interface{}{"updated": true})
	return nil
}

/* ---------------------------------- 密钥 ---------------------------------- */

func (s *Server) keys(w http.ResponseWriter, r *http.Request) error {
	merchantID := int64(httpx.QueryInt(r, "merchant_id", 0))
	page := httpx.QueryInt(r, "page", 1)
	size := httpx.QueryInt(r, "size", 20)

	list, total, err := s.store.ListAPIKeys(r.Context(), merchantID, page, size)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"total": total, "page": page, "size": size, "list": list})
	return nil
}

type createKeyRequest struct {
	MerchantID int64  `json:"merchant_id"`
	Name       string `json:"name"`
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) error {
	var req createKeyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.MerchantID <= 0 {
		return httpx.BadRequest("请选择商户")
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "default"
	}

	if _, err := s.store.MerchantByID(r.Context(), req.MerchantID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("商户不存在")
		}
		return err
	}

	accessKey, key, err := s.issueKey(r, req.MerchantID, strings.TrimSpace(req.Name))
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"access_key": accessKey, "key": key})
	return nil
}

// issueKey 生成密钥并入库，明文只在本次响应中返回。
func (s *Server) issueKey(r *http.Request, merchantID int64, name string) (string, *model.APIKey, error) {
	accessKey, err := middleware.GenerateAccessKey()
	if err != nil {
		return "", nil, err
	}
	key, err := s.store.CreateAPIKey(r.Context(), merchantID, name, accessKey[:12], middleware.HashKey(accessKey))
	if err != nil {
		return "", nil, err
	}
	return accessKey, key, nil
}

func (s *Server) keyStatus(w http.ResponseWriter, r *http.Request) error {
	var req idStatusRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少密钥 ID")
	}
	if req.Status != 0 && req.Status != 1 {
		return httpx.BadRequest("status 只能是 0 或 1")
	}

	if err := s.store.SetAPIKeyStatus(r.Context(), req.ID, req.Status); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("密钥不存在")
		}
		return err
	}
	httpx.JSON(w, map[string]interface{}{"updated": true})
	return nil
}

type idRequest struct {
	ID int64 `json:"id"`
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少密钥 ID")
	}

	if err := s.store.DeleteAPIKey(r.Context(), req.ID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("密钥不存在")
		}
		return err
	}
	httpx.JSON(w, map[string]interface{}{"deleted": true})
	return nil
}

/* ---------------------------------- 音乐创作 ---------------------------------- */

type adminGenerateRequest struct {
	musicreq.Generate
	MerchantID int64 `json:"merchant_id"`
	// VoiceID 选了音色时，歌曲生成完成后自动用该音色翻唱：0 为官方默认音色，大于 0 为音色库里的音色，不传则不翻唱
	VoiceID *int64 `json:"voice_id"`
}

// generateVersions 生成音乐一次产出的版本数，自动翻唱按版本数计费
const generateVersions = 2

// generateMusic 由运营在后台代商户发起生成，积分从该商户账户扣除。
func (s *Server) generateMusic(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req adminGenerateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}

	payload, err := req.Payload()
	if err != nil {
		return err
	}
	// 标记来源，便于在任务列表里区分后台代建与商户自助调用
	payload["created_by"] = "admin:" + user.Username

	var coverCost int64
	if req.VoiceID != nil {
		choice, err := s.voiceChoice(r, *req.VoiceID, merchant.ID, *req.MakeInstrumental)
		if err != nil {
			return err
		}
		payload[task.VoicePayloadKey] = choice
		// 翻唱在歌曲生成完成后才扣费，提交前先确认余额够付全程，免得歌做好了却翻唱不了
		coverCost = model.PriceOf(model.KindVoiceCover) * generateVersions
		if need := model.PriceOf(model.KindGenerate) + coverCost; merchant.Points < need {
			return httpx.NoPoints(fmt.Sprintf("积分不足：生成加两个版本的音色翻唱共需 %d 积分，当前余额 %d", need, merchant.Points))
		}
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindGenerate, payload, 0)
	if err != nil {
		return err
	}

	balance, _ := s.store.Balance(r.Context(), merchant.ID)
	httpx.JSON(w, map[string]interface{}{
		"task_ids":   ids,
		"balance":    balance,
		"cost":       model.PriceOf(model.KindGenerate),
		"cover_cost": coverCost,
	})
	return nil
}

// voiceChoice 校验创作时选的音色并取出模型名。
func (s *Server) voiceChoice(r *http.Request, voiceID, merchantID int64, instrumental bool) (*task.VoiceChoice, error) {
	if instrumental {
		return nil, httpx.BadRequest("纯音乐没有人声，不能指定音色")
	}
	if voiceID == 0 {
		return &task.VoiceChoice{ModelName: defaultVoiceModel, Name: defaultVoiceName}, nil
	}
	modelName, name, err := s.voiceOf(r, voiceID, merchantID)
	if err != nil {
		return nil, err
	}
	return &task.VoiceChoice{VoiceID: voiceID, ModelName: modelName, Name: name}, nil
}

// activeMerchant 取出后台代建任务的归属商户，并确认其处于启用状态。
func (s *Server) activeMerchant(r *http.Request, id int64) (*model.Merchant, error) {
	if id <= 0 {
		return nil, httpx.BadRequest("请选择归属商户")
	}
	merchant, err := s.store.MerchantByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("商户不存在")
		}
		return nil, err
	}
	if merchant.Status != 1 {
		return nil, httpx.BadRequest("该商户已被禁用，无法发起创作")
	}
	return merchant, nil
}

type adminUploadRequest struct {
	MerchantID     int64  `json:"merchant_id"`
	AudioURL       string `json:"audio_url"`
	CopyrightAudio bool   `json:"copyright_audio"`
}

// uploadMusic 上传参考音频，完成后得到的 custom_id 可直接用于翻唱或延长。
func (s *Server) uploadMusic(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req adminUploadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	req.AudioURL = strings.TrimSpace(req.AudioURL)
	if !strings.HasPrefix(req.AudioURL, "http://") && !strings.HasPrefix(req.AudioURL, "https://") {
		return httpx.BadRequest("音频地址必须是可公开访问的 http/https 链接")
	}

	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}

	var surcharge int64
	if req.CopyrightAudio {
		surcharge = model.CopyrightAudioSurcharge
	}
	payload := map[string]interface{}{
		"audio_url":      req.AudioURL,
		"copyrightAudio": req.CopyrightAudio,
		"created_by":     "admin:" + user.Username,
	}

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindUpload, payload, surcharge)
	if err != nil {
		return err
	}

	balance, _ := s.store.Balance(r.Context(), merchant.ID)
	httpx.JSON(w, map[string]interface{}{
		"task_id": ids[0],
		"balance": balance,
		"cost":    model.PriceOf(model.KindUpload) + surcharge,
	})
	return nil
}

// deleteSong 删除作品及其 MV 等衍生任务记录；上游 CDN 上的文件不受影响。
func (s *Server) deleteSong(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少作品 ID")
	}

	// 先记下平台副本，删除记录后一并清理
	var files *model.ArchivedFiles
	if t, _, err := s.store.AdminTaskByID(r.Context(), req.ID); err == nil && t.FileInfo != nil {
		files = t.FileInfo.Archived
	}

	deleted, err := s.store.DeleteSong(r.Context(), req.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("作品不存在或已删除")
		}
		return err
	}
	s.archive.Delete(r.Context(), files)
	httpx.JSON(w, map[string]interface{}{"deleted": deleted})
	return nil
}

// SetArchiver 设置作品转存器，删除作品时用来清理 COS 副本。
func (s *Server) SetArchiver(a *archive.Archiver) { s.archive = a }

// songs 列出作品库：已完成且带 custom_id 的歌曲，含封面、音频与 MV 状态。
func (s *Server) songs(w http.ResponseWriter, r *http.Request) error {
	filter := model.SongFilter{
		MerchantID: int64(httpx.QueryInt(r, "merchant_id", 0)),
		Keyword:    strings.TrimSpace(r.URL.Query().Get("keyword")),
		Page:       httpx.QueryInt(r, "page", 1),
		Size:       httpx.QueryInt(r, "size", 12),
	}

	list, total, err := s.store.ListSongs(r.Context(), filter)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{
		"total": total, "page": filter.Page, "size": filter.Size, "list": list,
	})
	return nil
}

type adminVideoRequest struct {
	// TaskID 是本平台的歌曲任务编号，suno_id 与上游任务号由服务端自动补齐
	TaskID int64 `json:"task_id"`
}

// generateVideo 为指定作品生成 MV。
func (s *Server) generateVideo(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req adminVideoRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.TaskID <= 0 {
		return httpx.BadRequest("请选择要生成 MV 的作品")
	}

	source, _, err := s.store.AdminTaskByID(r.Context(), req.TaskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("作品不存在")
		}
		return err
	}
	if source.Status != model.StatusCompleted {
		return httpx.BadRequest("作品尚未生成完成，无法制作 MV")
	}
	if source.CustomID == nil || *source.CustomID == "" {
		return httpx.BadRequest("该作品没有 custom_id，无法制作 MV")
	}
	if source.ProviderTaskID == "" {
		return httpx.BadRequest("该作品缺少上游任务号，无法制作 MV")
	}

	merchant, err := s.store.MerchantByID(r.Context(), source.MerchantID)
	if err != nil {
		return err
	}
	if merchant.Status != 1 {
		return httpx.BadRequest("该作品所属商户已被禁用")
	}

	// 上游要的是它自己的任务号，不是本平台的编号
	payload := map[string]interface{}{
		"task_id":    source.ProviderTaskID,
		"suno_id":    *source.CustomID,
		"created_by": "admin:" + user.Username,
	}

	ids, err := s.tasks.Submit(r.Context(), source.MerchantID, model.KindVideo, payload, 0)
	if err != nil {
		return err
	}

	balance, _ := s.store.Balance(r.Context(), source.MerchantID)
	httpx.JSON(w, map[string]interface{}{
		"task_ids": ids,
		"balance":  balance,
		"cost":     model.PriceOf(model.KindVideo),
	})
	return nil
}

/* ---------------------------------- 图片与视频创作 ---------------------------------- */

// capabilities 返回图片与视频的全部生成能力，供后台动态渲染表单。
func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) error {
	group := strings.TrimSpace(r.URL.Query().Get("group"))
	list := model.Capabilities(group)
	httpx.JSON(w, map[string]interface{}{"list": list, "total": len(list)})
	return nil
}

type studioRequest struct {
	MerchantID int64                  `json:"merchant_id"`
	Kind       string                 `json:"kind"`
	Payload    map[string]interface{} `json:"payload"`
}

// studioGenerate 通用创作入口：按能力定义校验必填项后提交到上游。
func (s *Server) studioGenerate(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req studioRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.MerchantID <= 0 {
		return httpx.BadRequest("请选择归属商户")
	}

	cap, ok := model.CapabilityOf(model.TaskKind(req.Kind))
	if !ok {
		return httpx.BadRequest("未知的生成能力：" + req.Kind)
	}

	merchant, err := s.store.MerchantByID(r.Context(), req.MerchantID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("商户不存在")
		}
		return err
	}
	if merchant.Status != 1 {
		return httpx.BadRequest("该商户已被禁用，无法发起创作")
	}

	payload := map[string]interface{}{}
	for _, p := range cap.Params {
		v, exists := req.Payload[p.Name]
		if !exists || isBlank(v) {
			if p.Required {
				label := p.Label
				if label == "" {
					label = p.Name
				}
				return httpx.BadRequest("缺少必填参数：" + label)
			}
			continue
		}
		payload[p.Name] = v
	}
	payload["created_by"] = "admin:" + user.Username

	ids, err := s.tasks.Submit(r.Context(), merchant.ID, cap.Kind, payload, 0)
	if err != nil {
		return err
	}

	balance, _ := s.store.Balance(r.Context(), merchant.ID)
	httpx.JSON(w, map[string]interface{}{
		"task_ids": ids,
		"balance":  balance,
		"cost":     cap.Price,
		"label":    cap.Label,
	})
	return nil
}

// isBlank 判断表单值是否为空，空字符串与空数组都算没填。
func isBlank(v interface{}) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	case []interface{}:
		return len(val) == 0
	}
	return false
}

/* ---------------------------------- 任务 ---------------------------------- */

func (s *Server) taskList(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	filter := model.TaskFilter{
		MerchantID: int64(httpx.QueryInt(r, "merchant_id", 0)),
		Kind:       strings.TrimSpace(query.Get("kind")),
		Status:     strings.TrimSpace(query.Get("status")),
		Keyword:    strings.TrimSpace(query.Get("keyword")),
		Start:      strings.TrimSpace(query.Get("start")),
		End:        strings.TrimSpace(query.Get("end")),
		Page:       httpx.QueryInt(r, "page", 1),
		Size:       httpx.QueryInt(r, "size", 20),
	}

	list, total, err := s.store.ListTasks(r.Context(), filter)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{
		"total": total, "page": filter.Page, "size": filter.Size, "list": list,
	})
	return nil
}

func (s *Server) taskDetail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.QueryInt64(r, "id")
	if err != nil {
		return err
	}

	task, request, err := s.store.AdminTaskByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("任务不存在")
		}
		return err
	}
	out := map[string]interface{}{"task": task, "request_payload": request}
	// 创作时选了音色的歌曲，带上自动翻唱的进度
	if task.Task.Kind == model.KindGenerate {
		if cover, err := s.store.VoiceCoverOf(r.Context(), id); err == nil {
			out["voice_cover"] = cover
		}
	}
	httpx.JSON(w, out)
	return nil
}

// retryTask 免费重试失败的任务（失败不退积分）。
func (s *Server) retryTask(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少任务 ID")
	}
	t, err := s.tasks.Retry(r.Context(), 0, req.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"task": t, "max_retries": s.cfg.TaskMaxRetries})
	return nil
}

// refundTask 人工退款：失败任务默认不退积分，这里供客服特殊情况下兜底。
func (s *Server) refundTask(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少任务 ID")
	}

	task, _, err := s.store.AdminTaskByID(r.Context(), req.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("任务不存在")
		}
		return err
	}
	if task.PointsRefunded {
		return httpx.BadRequest("该任务已退过积分")
	}
	if task.PointsCost <= 0 {
		return httpx.BadRequest("该任务未消耗积分，无需退还")
	}

	if err := s.store.Refund(r.Context(), req.ID, "人工退还（操作人："+user.Username+"）"); err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"refunded": true, "points": task.PointsCost})
	return nil
}

/* ---------------------------------- 积分 ---------------------------------- */

func (s *Server) pointLogs(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	filter := model.PointLogFilter{
		MerchantID: int64(httpx.QueryInt(r, "merchant_id", 0)),
		Type:       httpx.QueryInt(r, "type", 0),
		Start:      strings.TrimSpace(query.Get("start")),
		End:        strings.TrimSpace(query.Get("end")),
		Page:       httpx.QueryInt(r, "page", 1),
		Size:       httpx.QueryInt(r, "size", 20),
	}

	list, total, err := s.store.ListPointLogs(r.Context(), filter)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{
		"total": total, "page": filter.Page, "size": filter.Size, "list": list,
	})
	return nil
}

type adjustRequest struct {
	MerchantID int64  `json:"merchant_id"`
	Points     int64  `json:"points"`
	Type       int    `json:"type"`
	Remark     string `json:"remark"`
}

// adjustPoints 充值（type=2）或手动调整（type=3），points 为负表示扣减。
func (s *Server) adjustPoints(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req adjustRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.MerchantID <= 0 {
		return httpx.BadRequest("请选择商户")
	}
	if req.Points == 0 {
		return httpx.BadRequest("变动积分不能为 0")
	}
	if req.Type != model.PointRecharge && req.Type != model.PointAdjust {
		return httpx.BadRequest("type 只能是 2（充值）或 3（手动调整）")
	}
	if req.Type == model.PointRecharge && req.Points < 0 {
		return httpx.BadRequest("充值积分必须为正数")
	}

	remark := strings.TrimSpace(req.Remark)
	if remark == "" {
		remark = map[int]string{model.PointRecharge: "后台充值", model.PointAdjust: "后台调整"}[req.Type]
	}
	remark += "（操作人：" + user.Username + "）"

	balance, err := s.store.Recharge(r.Context(), req.MerchantID, req.Points, req.Type, remark)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("商户不存在")
		}
		if errors.Is(err, storage.ErrInsufficientPoints) {
			return httpx.BadRequest("扣减后余额会为负数，请调整数值")
		}
		return err
	}
	httpx.JSON(w, map[string]interface{}{"balance": balance})
	return nil
}
