package adminapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lepro/suno-open-api/internal/api"
	"github.com/lepro/suno-open-api/internal/certificate"
	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/storage"
)

// SetCertificates 设置创作证明服务。
func (s *Server) SetCertificates(c *certificate.Service) { s.certs = c }

func (s *Server) registerCertificates(r *httpx.Router, auth httpx.Middleware) {
	if s.certs == nil {
		return
	}
	r.GET("/admin/api/songs/certificate", s.certificateStatus, auth)
	r.POST("/admin/api/songs/certificate", s.issueCertificate, auth)
	r.GET("/admin/api/songs/certificate/file", s.certificateFile, auth)
}

// certificateStatus 作品的证明签发情况，供签发弹窗展示价格、余额与已有证明。
func (s *Server) certificateStatus(w http.ResponseWriter, r *http.Request) error {
	taskID, err := httpx.QueryInt64(r, "task_id")
	if err != nil {
		return err
	}
	row, _, err := s.store.AdminTaskByID(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("作品不存在")
		}
		return err
	}
	cert, err := s.certs.ByTask(r.Context(), taskID)
	if err != nil {
		return err
	}
	balance, _ := s.store.Balance(r.Context(), row.MerchantID)

	out := map[string]interface{}{
		"price":         s.certs.Price(),
		"merchant_id":   row.MerchantID,
		"merchant_name": row.MerchantName,
		"balance":       balance,
		"certificate":   nil,
	}
	if cert != nil {
		out["certificate"] = cert
		out["verify_url"] = s.certs.VerifyLink(cert.No)
	}
	httpx.JSON(w, out)
	return nil
}

type issueCertificateRequest struct {
	TaskID int64  `json:"task_id"`
	Author string `json:"author"`
}

// issueCertificate 代商户为作品签发证明，扣商户积分；已签发过的直接返回，不重复扣费。
func (s *Server) issueCertificate(w http.ResponseWriter, r *http.Request) error {
	var req issueCertificateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.TaskID <= 0 {
		return httpx.BadRequest("请选择作品")
	}

	existing, err := s.certs.ByTask(r.Context(), req.TaskID)
	if err != nil {
		return err
	}
	if existing == nil {
		// 新签发要扣费，商户被禁用时不允许
		row, _, err := s.store.AdminTaskByID(r.Context(), req.TaskID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return httpx.NotFound("作品不存在")
			}
			return err
		}
		if _, err := s.activeMerchant(r, row.MerchantID); err != nil {
			return err
		}
	}

	res, err := s.certs.Issue(r.Context(), 0, req.TaskID, strings.TrimSpace(req.Author))
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{
		"certificate": res.Certificate,
		"charged":     res.Charged,
		"balance":     res.Balance,
		"verify_url":  s.certs.VerifyLink(res.Certificate.No),
	})
	return nil
}

// certificateFile 下载证明 PDF。
func (s *Server) certificateFile(w http.ResponseWriter, r *http.Request) error {
	no := strings.TrimSpace(r.URL.Query().Get("no"))
	if no == "" {
		return httpx.BadRequest("缺少证书编号")
	}
	c, err := s.certs.Get(r.Context(), 0, no)
	if err != nil {
		return err
	}
	return api.WritePDF(w, s.certs, c)
}
