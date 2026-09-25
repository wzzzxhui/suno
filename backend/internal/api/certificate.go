package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lepro/suno-open-api/internal/certificate"
	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/middleware"
	"github.com/lepro/suno-open-api/internal/model"
)

// SetCertificates 设置创作证明服务；未设置时不注册相关接口。
func (s *Server) SetCertificates(c *certificate.Service) { s.certs = c }

func (s *Server) registerCertificates(r *httpx.Router, auth, limit httpx.Middleware) {
	if s.certs == nil {
		return
	}
	r.POST("/api/v1/music/certificate", s.issueCertificate, auth, limit)
	r.GET("/api/v1/music/certificate/download", s.downloadCertificate, auth, limit)
	// 公开核验：扫证书上的二维码或在公开站输入编号，免鉴权，按来源 IP 限流
	r.GET("/api/v1/certificate/verify", s.verifyCertificate, middleware.IPRateLimit(30))
}

type certificateRequest struct {
	SunoID string `json:"suno_id"`
	TaskID int64  `json:"task_id"`
	Author string `json:"author"`
}

// issueCertificate 为作品签发创作证明：首次扣费，之后重复调用返回同一份证明且不再扣费。
func (s *Server) issueCertificate(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}
	var req certificateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}

	taskID := req.TaskID
	if strings.TrimSpace(req.SunoID) != "" {
		if err := requireClipID(req.SunoID, "suno_id"); err != nil {
			return err
		}
		if taskID, err = s.certs.SongTaskID(r.Context(), merchant.ID, req.SunoID); err != nil {
			return err
		}
	}
	if taskID <= 0 {
		return httpx.BadRequest("缺少必填参数 suno_id")
	}

	res, err := s.certs.Issue(r.Context(), merchant.ID, taskID, req.Author)
	if err != nil {
		return err
	}
	httpx.JSON(w, certificateView(s.certs, res.Certificate, map[string]interface{}{
		"charged":          res.Charged,
		"remaining_points": res.Balance,
		"download_path":    "/api/v1/music/certificate/download?certificate_no=" + url.QueryEscape(res.Certificate.No),
	}))
	return nil
}

// downloadCertificate 下载证明 PDF，不扣费。
func (s *Server) downloadCertificate(w http.ResponseWriter, r *http.Request) error {
	merchant, err := middleware.MerchantFrom(r)
	if err != nil {
		return err
	}
	no := strings.TrimSpace(r.URL.Query().Get("certificate_no"))
	if no == "" {
		return httpx.BadRequest("缺少参数 certificate_no")
	}
	c, err := s.certs.Get(r.Context(), merchant.ID, no)
	if err != nil {
		return err
	}
	return WritePDF(w, s.certs, c)
}

// verifyCertificate 公开核验证明，只返回不涉及商户隐私的字段。
func (s *Server) verifyCertificate(w http.ResponseWriter, r *http.Request) error {
	no := strings.TrimSpace(r.URL.Query().Get("certificate_no"))
	if no == "" {
		no = strings.TrimSpace(r.URL.Query().Get("no"))
	}
	if no == "" {
		return httpx.BadRequest("缺少参数 certificate_no")
	}
	c, err := s.certs.Get(r.Context(), 0, no)
	if err != nil {
		return err
	}
	httpx.JSON(w, s.certs.Public(c))
	return nil
}

// certificateView 签发结果的对外字段。
func certificateView(svc *certificate.Service, c *model.Certificate, extra map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"certificate_no":  c.No,
		"task_id":         c.TaskID,
		"suno_id":         c.SunoID,
		"title":           c.Title,
		"author":          c.Author,
		"duration":        c.Duration,
		"audio_sha256":    c.AudioSHA256,
		"song_created_at": c.SongCreatedAt,
		"issued_at":       c.IssuedAt,
		"points_cost":     c.PointsCost,
		"verify_url":      svc.VerifyLink(c.No),
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// WritePDF 以附件形式输出证明 PDF，运营后台也用它。
func WritePDF(w http.ResponseWriter, svc *certificate.Service, c *model.Certificate) error {
	pdf, err := svc.PDF(c)
	if err != nil {
		return err
	}
	name := certificate.FileName(c)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	w.Header().Set("Content-Disposition",
		`attachment; filename="certificate-`+c.No+`.pdf"; filename*=UTF-8''`+url.PathEscape(name))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(pdf)
	return nil
}
