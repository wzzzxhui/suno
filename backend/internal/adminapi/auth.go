// Package adminapi 提供运营后台的接口：登录、商户、密钥、任务、积分与统计。
package adminapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/storage"
)

const pbkdf2Iterations = 120000

/* ---------------------------------- 口令哈希 ---------------------------------- */

// HashPassword 生成 pbkdf2$迭代次数$盐$哈希 形式的口令哈希。
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := pbkdf2SHA256([]byte(password), salt, pbkdf2Iterations, 32)
	return fmt.Sprintf("pbkdf2$%d$%s$%s", pbkdf2Iterations, hex.EncodeToString(salt), hex.EncodeToString(sum)), nil
}

// VerifyPassword 校验口令，使用恒定时间比较避免计时攻击。
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(got, expected) == 1
}

// pbkdf2SHA256 是 PBKDF2-HMAC-SHA256 的标准库实现，避免为此引入第三方依赖。
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	hashLen := sha256.Size
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)
	buf := make([]byte, 4)

	for block := 1; block <= blocks; block++ {
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)

		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write(buf)
		u := mac.Sum(nil)

		acc := make([]byte, len(u))
		copy(acc, u)

		for i := 1; i < iterations; i++ {
			mac.Reset()
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range acc {
				acc[j] ^= u[j]
			}
		}
		out = append(out, acc...)
	}
	return out[:keyLen]
}

/* ---------------------------------- 登录令牌 ---------------------------------- */

type tokenPayload struct {
	UID      int64  `json:"uid"`
	Username string `json:"username"`
	Exp      int64  `json:"exp"`
}

// signToken 生成 base64(payload).签名 形式的令牌，仅依赖标准库。
func signToken(secret string, uid int64, username string, ttl time.Duration) (string, time.Time, error) {
	expiresAt := time.Now().Add(ttl)
	raw, err := json.Marshal(tokenPayload{UID: uid, Username: username, Exp: expiresAt.Unix()})
	if err != nil {
		return "", time.Time{}, err
	}

	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, expiresAt, nil
}

// parseToken 校验签名与有效期，返回令牌载荷。
func parseToken(secret, token string) (*tokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("令牌格式错误")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return nil, errors.New("令牌签名无效")
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("令牌内容无法解析")
	}
	var payload tokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("令牌内容无法解析")
	}
	if time.Now().Unix() > payload.Exp {
		return nil, errors.New("登录已过期，请重新登录")
	}
	return &payload, nil
}

/* ---------------------------------- 鉴权中间件 ---------------------------------- */

type ctxKey string

const adminKey ctxKey = "admin_user"

// adminFrom 从上下文取出当前登录的后台账号。
func adminFrom(r *http.Request) (*model.AdminUser, error) {
	u, ok := r.Context().Value(adminKey).(*model.AdminUser)
	if !ok || u == nil {
		return nil, httpx.Unauthorized("请先登录")
	}
	return u, nil
}

// authMiddleware 校验 Authorization: Bearer <token>。
func (s *Server) authMiddleware(next httpx.Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		raw := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
			return httpx.Unauthorized("请先登录")
		}

		payload, err := parseToken(s.secret, strings.TrimSpace(raw[7:]))
		if err != nil {
			return httpx.Unauthorized(err.Error())
		}

		user, err := s.store.AdminByID(r.Context(), payload.UID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return httpx.Unauthorized("账号不存在")
			}
			return err
		}
		if user.Status != 1 {
			return httpx.Unauthorized("账号已被禁用")
		}

		ctx := context.WithValue(r.Context(), adminKey, user)
		return next(w, r.WithContext(ctx))
	}
}
