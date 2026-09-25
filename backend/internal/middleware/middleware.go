// Package middleware 提供鉴权、限流、CORS 与访问日志。
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/storage"
)

type ctxKey string

const merchantKey ctxKey = "merchant"

// HashKey 计算 access_key 的存储哈希。
func HashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// GenerateAccessKey 生成 sk- 开头的随机商户密钥。
func GenerateAccessKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := base64.StdEncoding.EncodeToString(buf)
	return "sk-" + strings.NewReplacer("+", "", "/", "", "=", "").Replace(raw), nil
}

// MerchantFrom 从请求上下文取出已鉴权的商户。
func MerchantFrom(r *http.Request) (*model.Merchant, error) {
	m, ok := r.Context().Value(merchantKey).(*model.Merchant)
	if !ok || m == nil {
		return nil, httpx.Unauthorized("鉴权信息缺失")
	}
	return m, nil
}

// Auth 校验 Authorization: Bearer <access_key>。
func Auth(store *storage.Store) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			raw := strings.TrimSpace(r.Header.Get("Authorization"))
			if raw == "" {
				return httpx.Unauthorized("缺少 Authorization 请求头")
			}
			if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
				return httpx.Unauthorized("Authorization 格式应为：Bearer <access_key>")
			}

			accessKey := strings.TrimSpace(raw[7:])
			if accessKey == "" {
				return httpx.Unauthorized("access_key 为空")
			}

			merchant, keyID, err := store.MerchantByKeyHash(r.Context(), HashKey(accessKey))
			if err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					return httpx.Unauthorized("access_key 无效或已被禁用")
				}
				return err
			}
			if merchant.Status != 1 {
				return httpx.Unauthorized("商户已被禁用")
			}

			store.TouchAPIKey(r.Context(), keyID)
			ctx := context.WithValue(r.Context(), merchantKey, merchant)
			return next(w, r.WithContext(ctx))
		}
	}
}

// RateLimit 按商户做每分钟固定窗口限流，超出返回 429。
func RateLimit(perMinute int) httpx.Middleware {
	type window struct {
		count int
		start time.Time
	}

	var (
		mu      sync.Mutex
		buckets = make(map[int64]*window)
	)

	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			if perMinute <= 0 {
				return next(w, r)
			}
			merchant, err := MerchantFrom(r)
			if err != nil {
				return next(w, r)
			}

			mu.Lock()
			bucket, ok := buckets[merchant.ID]
			now := time.Now()
			if !ok || now.Sub(bucket.start) >= time.Minute {
				bucket = &window{count: 0, start: now}
				buckets[merchant.ID] = bucket
			}
			bucket.count++
			exceeded := bucket.count > perMinute
			mu.Unlock()

			if exceeded {
				return httpx.TooManyRequests("请求过于频繁，请降低调用频率")
			}
			return next(w, r)
		}
	}
}

// IPRateLimit 按来源 IP 限流，用于免鉴权的公开接口。
func IPRateLimit(perMinute int) httpx.Middleware {
	type window struct {
		count int
		start time.Time
	}

	var (
		mu      sync.Mutex
		buckets = make(map[string]*window)
	)

	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			ip := ClientIP(r)
			now := time.Now()

			mu.Lock()
			bucket, ok := buckets[ip]
			if !ok || now.Sub(bucket.start) >= time.Minute {
				// 顺手清掉过期窗口，避免表无限增长
				if len(buckets) > 10000 {
					for k, b := range buckets {
						if now.Sub(b.start) >= time.Minute {
							delete(buckets, k)
						}
					}
				}
				bucket = &window{start: now}
				buckets[ip] = bucket
			}
			bucket.count++
			exceeded := bucket.count > perMinute
			mu.Unlock()

			if exceeded {
				return httpx.TooManyRequests("请求过于频繁，请稍后再试")
			}
			return next(w, r)
		}
	}
}

// ClientIP 取请求来源 IP。只有直连方是本机（前面有 Nginx 反代）时才信任 X-Real-IP / X-Forwarded-For，
// 避免直连时被伪造请求头绕过限流。
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
			return v
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			return strings.TrimSpace(strings.Split(v, ",")[0])
		}
	}
	return host
}

// CORS 允许前端页面直接调用接口。
func CORS(origins []string) httpx.Middleware {
	allowAll := len(origins) == 0
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}

	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAll || allowed[origin]) {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return nil
			}
			return next(w, r)
		}
	}
}

// Logger 打印访问日志。
func Logger() httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			start := time.Now()
			err := next(w, r)
			log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), time.Since(start).Round(time.Millisecond))
			return err
		}
	}
}

// Recover 捕获 panic，避免单个请求打挂进程。
func Recover() httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("[panic] %s %s: %v", r.Method, r.URL.Path, rec)
					err = httpx.Internal("服务内部错误")
				}
			}()
			return next(w, r)
		}
	}
}
