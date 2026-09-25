package adminapi

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/mv"
	"github.com/lepro/suno-open-api/internal/storage"
)

/* ---------------------------------- 长 MV ---------------------------------- */

var (
	mvRatios      = map[string]bool{"16:9": true, "9:16": true, "1:1": true, "4:3": true, "3:4": true}
	mvResolutions = map[string]bool{"480p": true, "720p": true, "1080p": true}
)

// mvProjects 分页列出 MV，并附带定价与限制供页面展示。
func (s *Server) mvProjects(w http.ResponseWriter, r *http.Request) error {
	page, size := httpx.QueryInt(r, "page", 1), httpx.QueryInt(r, "size", 10)
	list, total, err := s.mv.List(r.Context(), int64(httpx.QueryInt(r, "merchant_id", 0)), page, size)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{
		"list": list, "total": total, "page": page, "size": size,
		"subtitles":    s.mv.SubtitlesReady(),
		"max_duration": s.mv.MaxDuration(),
		"ready":        s.mv.Ready(),
	})
	return nil
}

func (s *Server) mvProject(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.QueryInt64(r, "id")
	if err != nil {
		return err
	}
	p, err := s.mv.Get(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.JSON(w, p)
	return nil
}

type mvCreateRequest struct {
	MerchantID  int64        `json:"merchant_id"`
	SongTaskID  int64        `json:"song_task_id"`
	Mode        string       `json:"mode"`
	Tier        string       `json:"tier"`
	ReuseChorus bool         `json:"reuse_chorus"`
	Subtitles   bool         `json:"subtitles"`
	StyleNote   string       `json:"style_note"`
	Ratio       string       `json:"ratio"`
	Resolution  string       `json:"resolution"`
	UseCover    bool         `json:"use_cover"`
	Look        model.MVLook `json:"look"`
}

// mvCreate 新建 MV：auto 模式写完分镜直接扣费生成，manual 模式只生成草稿。
func (s *Server) mvCreate(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}
	var req mvCreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.SongTaskID <= 0 {
		return httpx.BadRequest("请选择要制作 MV 的作品")
	}
	if req.Ratio == "" {
		req.Ratio = "16:9"
	}
	if req.Resolution == "" {
		req.Resolution = "480p"
	}
	if req.Tier == "" {
		req.Tier = "standard"
	}
	if !mvRatios[req.Ratio] || !mvResolutions[req.Resolution] {
		return httpx.BadRequest("不支持的画面比例或分辨率")
	}
	if len([]rune(req.StyleNote)) > 500 {
		return httpx.BadRequest("画面要求最多 500 字")
	}
	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}

	p, err := s.mv.Draft(r.Context(), mv.DraftRequest{
		MerchantID:  merchant.ID,
		SongTaskID:  req.SongTaskID,
		Mode:        req.Mode,
		Tier:        req.Tier,
		ReuseChorus: req.ReuseChorus,
		Subtitles:   req.Subtitles,
		StyleNote:   req.StyleNote,
		Ratio:       req.Ratio,
		Resolution:  req.Resolution,
		UseCover:    req.UseCover,
		Look:        req.Look,
		CreatedBy:   "admin:" + user.Username,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, p)
	return nil
}

type mvQuoteRequest struct {
	MerchantID  int64  `json:"merchant_id"`
	SongTaskID  int64  `json:"song_task_id"`
	ReuseChorus bool   `json:"reuse_chorus"`
	Resolution  string `json:"resolution"`
	WithRefs    bool   `json:"with_refs"`
}

// mvQuote 一次返回三个档位的报价与镜头构成，供页面在开始前展示。
func (s *Server) mvQuote(w http.ResponseWriter, r *http.Request) error {
	var req mvQuoteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.SongTaskID <= 0 {
		return httpx.BadRequest("请选择作品")
	}
	if !mvResolutions[req.Resolution] {
		return httpx.BadRequest("不支持的分辨率")
	}
	out := map[string]interface{}{}
	for _, tier := range []string{"economy", "standard", "premium"} {
		q, err := s.mv.Quote(r.Context(), mv.QuoteRequest{
			MerchantID: req.MerchantID, SongTaskID: req.SongTaskID, Tier: tier,
			ReuseChorus: req.ReuseChorus, Resolution: req.Resolution, WithRefs: req.WithRefs,
		})
		if err != nil {
			return err
		}
		out[tier] = q
	}
	httpx.JSON(w, out)
	return nil
}

type mvRewriteRequest struct {
	ID        int64         `json:"id"`
	StyleNote string        `json:"style_note"`
	Look      *model.MVLook `json:"look"`
}

// mvRewrite 让 AI 按新的画面要求（及形象设定）重写草稿分镜。
func (s *Server) mvRewrite(w http.ResponseWriter, r *http.Request) error {
	var req mvRewriteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if len([]rune(req.StyleNote)) > 500 {
		return httpx.BadRequest("画面要求最多 500 字")
	}
	p, err := s.mv.Rewrite(r.Context(), req.ID, req.StyleNote, req.Look)
	if err != nil {
		return err
	}
	httpx.JSON(w, p)
	return nil
}

type mvStoryboardRequest struct {
	ID          int64  `json:"id"`
	VisualBible string `json:"visual_bible"`
	Segments    []struct {
		ID     int64  `json:"id"`
		Prompt string `json:"prompt"`
	} `json:"segments"`
}

// mvSaveStoryboard 保存运营精修后的分镜。
func (s *Server) mvSaveStoryboard(w http.ResponseWriter, r *http.Request) error {
	var req mvStoryboardRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	prompts := make(map[int64]string, len(req.Segments))
	for _, seg := range req.Segments {
		prompts[seg.ID] = seg.Prompt
	}
	p, err := s.mv.SaveStoryboard(r.Context(), req.ID, strings.TrimSpace(req.VisualBible), prompts)
	if err != nil {
		return err
	}
	httpx.JSON(w, p)
	return nil
}

// mvStart 扣除固定价，开始生成草稿。
func (s *Server) mvStart(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	balance, err := s.mv.Start(r.Context(), req.ID)
	if err != nil {
		return err
	}
	p, err := s.mv.Get(r.Context(), req.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"project": p, "balance": balance, "cost": p.PointsCost})
	return nil
}

// mvRetry 重试失败的 MV。
func (s *Server) mvRetry(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	balance, cost, err := s.mv.Retry(r.Context(), req.ID)
	if err != nil {
		return err
	}
	p, err := s.mv.Get(r.Context(), req.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"project": p, "balance": balance, "cost": cost})
	return nil
}

func (s *Server) mvDelete(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := s.mv.Delete(r.Context(), req.ID); err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"deleted": true})
	return nil
}

// mvUploadRef 上传人物 / 服装参考图，存进 COS 后返回对象路径，建 MV 时随形象设定提交。
func (s *Server) mvUploadRef(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return httpx.BadRequest("参考图不能超过 10MB")
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	merchantID, _ := strconv.ParseInt(r.FormValue("merchant_id"), 10, 64)
	merchant, err := s.activeMerchant(r, merchantID)
	if err != nil {
		return err
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("请选择参考图")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 10<<20+1))
	if err != nil {
		return err
	}
	key, url, err := s.mv.SaveRef(r.Context(), merchant.ID, filepath.Ext(header.Filename), data)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"key": key, "url": url})
	return nil
}

type mvPortraitRequest struct {
	MerchantID int64        `json:"merchant_id"`
	Look       model.MVLook `json:"look"`
	StyleNote  string       `json:"style_note"`
}

// mvPortrait 按形象设定生成一张定妆照，按绘画能力的单价扣费；确认后作为参考图用于每个镜头。
func (s *Server) mvPortrait(w http.ResponseWriter, r *http.Request) error {
	var req mvPortraitRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}
	kind, payload, err := s.mv.PortraitPayload(merchant.ID, req.Look, req.StyleNote)
	if err != nil {
		return err
	}
	ids, err := s.tasks.Submit(r.Context(), merchant.ID, kind, payload, 0)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"task_id": ids[0], "cost": model.PriceOf(kind)})
	return nil
}

// mvPortraitStatus 查询定妆照进度；完成后把图片转存到 COS，返回可作为参考图的对象路径。
func (s *Server) mvPortraitStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.QueryInt64(r, "task_id")
	if err != nil {
		return err
	}
	t, _, err := s.store.AdminTaskByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("定妆照任务不存在")
		}
		return err
	}
	switch t.Status {
	case model.StatusCompleted:
		if t.FileInfo == nil || len(t.FileInfo.Images) == 0 {
			httpx.JSON(w, map[string]interface{}{"status": "failed", "reason": "生成完成但没有图片"})
			return nil
		}
		key, url, err := s.mv.ImportRef(r.Context(), t.MerchantID, t.FileInfo.Images[0])
		if err != nil {
			return err
		}
		httpx.JSON(w, map[string]interface{}{"status": "completed", "key": key, "url": url})
	case model.StatusFailed:
		httpx.JSON(w, map[string]interface{}{"status": "failed", "reason": t.ErrorMessage})
	default:
		httpx.JSON(w, map[string]interface{}{"status": "running"})
	}
	return nil
}
