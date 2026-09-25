package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// COSSigner 为私有 COS 存储桶里的对象生成带时效的下载地址，并支持上传（XML API 请求签名）。
// 规则见 https://cloud.tencent.com/document/product/436/7778
type COSSigner struct {
	SecretID  string
	SecretKey string
	Expire    time.Duration
}

// Enabled 返回是否配置了签名密钥；未配置时直接返回未签名地址。
func (c *COSSigner) Enabled() bool {
	return c != nil && c.SecretID != "" && c.SecretKey != ""
}

// Sign 给对象地址追加签名参数，rawURL 形如 https://bucket.cos.region.myqcloud.com/path/to/file。
func (c *COSSigner) Sign(rawURL string, now time.Time) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	expire := c.Expire
	if expire <= 0 {
		expire = time.Hour
	}

	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + c.authorization(http.MethodGet, u.Path, now, expire).Encode(), nil
}

// Put 把文件写入存储桶，objectURL 形如 https://bucket.cos.region.myqcloud.com/path/to/file。
func (c *COSSigner) Put(ctx context.Context, client *http.Client, objectURL, contentType string, body []byte) error {
	u, err := url.Parse(objectURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// 签名只需在请求发出前有效，给足上传大文件的时间；请求头里用未转义的原文
	auth, _ := url.QueryUnescape(c.authorization(http.MethodPut, u.Path, time.Now(), 10*time.Minute).Encode())
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", contentType)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("上传 COS 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("上传 COS 返回 HTTP %d: %s", resp.StatusCode, truncate(string(msg), 300))
	}
	return nil
}

// Delete 删除存储桶里的对象，对象不存在时同样视为成功。
func (c *COSSigner) Delete(ctx context.Context, client *http.Client, objectURL string) error {
	u, err := url.Parse(objectURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, objectURL, nil)
	if err != nil {
		return err
	}
	auth, _ := url.QueryUnescape(c.authorization(http.MethodDelete, u.Path, time.Now(), 10*time.Minute).Encode())
	req.Header.Set("Authorization", auth)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("删除 COS 对象失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("删除 COS 对象返回 HTTP %d: %s", resp.StatusCode, truncate(string(msg), 300))
	}
	return nil
}

// authorization 计算签名参数。不签任何请求参数与请求头，只签方法与路径。
func (c *COSSigner) authorization(method, path string, now time.Time, expire time.Duration) url.Values {
	keyTime := fmt.Sprintf("%d;%d", now.Unix(), now.Add(expire).Unix())
	signKey := hmacSHA1Hex(c.SecretKey, keyTime)
	httpString := strings.ToLower(method) + "\n" + path + "\n\n\n"
	stringToSign := "sha1\n" + keyTime + "\n" + sha1Hex(httpString) + "\n"

	q := url.Values{}
	q.Set("q-sign-algorithm", "sha1")
	q.Set("q-ak", c.SecretID)
	q.Set("q-sign-time", keyTime)
	q.Set("q-key-time", keyTime)
	q.Set("q-header-list", "")
	q.Set("q-url-param-list", "")
	q.Set("q-signature", hmacSHA1Hex(signKey, stringToSign))
	return q
}

func hmacSHA1Hex(key, msg string) string {
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
