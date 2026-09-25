// Package certificate 为作品签发创作证明：固化作品信息与音频指纹，按需绘制成 PDF，并提供公开核验。
//
// 证明是平台依据自身系统记录出具的自签证书，用于证明作品的创作时间、来源与音频文件未被改动，
// 不等同于著作权登记。
package certificate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lepro/suno-open-api/internal/archive"
	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/media"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/storage"
)

// Options 证明服务的配置。
type Options struct {
	Price     int64  // 每份价格（积分），首次签发收取
	Issuer    string // 签发单位，印在证书与印章上
	FontFile  string // 含中文的字体文件，支持 .ttf/.otf/.ttc
	VerifyURL string // 公开核验页地址，留空则证书上不印二维码
	// MP3 取作品的 MP3 文件，指纹按它计算
	MP3 func(ctx context.Context, t *model.Task) ([]byte, error)
	// MockAudio 为 true 时（mock provider），音频不可下载就用音频地址算指纹，便于本地联调
	MockAudio bool
}

// Service 创作证明服务。
type Service struct {
	store *storage.Store
	opts  Options
	fonts *fontCache
}

// New 创建证明服务。
func New(store *storage.Store, opts Options) *Service {
	if strings.TrimSpace(opts.Issuer) == "" {
		opts.Issuer = "SUNO API 开放平台"
	}
	return &Service{
		store: store,
		opts:  opts,
		fonts: &fontCache{path: opts.FontFile},
	}
}

// CheckFont 加载证书字体，启动时调用以便尽早发现配置问题。
func (s *Service) CheckFont() error {
	_, err := s.fonts.load()
	return err
}

// Price 每份证明的价格（积分）。
func (s *Service) Price() int64 { return s.opts.Price }

// Result 一次签发的结果。
type Result struct {
	Certificate *model.Certificate
	Charged     bool  // 本次是否扣费；作品已有证明时为 false
	Balance     int64 // 商户当前余额
}

// Issue 为作品签发证明。作品已有证明时直接返回，不重复扣费。
// merchantID 大于 0 时校验作品归属；author 为空时用商户名署名。
func (s *Service) Issue(ctx context.Context, merchantID, taskID int64, author string) (*Result, error) {
	row, request, err := s.store.AdminTaskByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("作品不存在")
		}
		return nil, err
	}
	if merchantID > 0 && row.MerchantID != merchantID {
		return nil, httpx.NotFound("作品不存在")
	}

	if prev, err := s.store.CertificateByTask(ctx, taskID); err == nil {
		balance, _ := s.store.Balance(ctx, row.MerchantID)
		return &Result{Certificate: prev, Balance: balance}, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}

	t := &row.Task
	if err := checkSong(t); err != nil {
		return nil, err
	}

	author = strings.TrimSpace(author)
	if author == "" {
		author = row.MerchantName
	}
	if utf8.RuneCountInString(author) > 50 {
		return nil, httpx.BadRequest("署名作者最多 50 个字")
	}

	c := &model.Certificate{
		MerchantID: row.MerchantID,
		TaskID:     t.ID,
		SunoID:     *t.CustomID,
		Author:     author,
		PointsCost: s.opts.Price,
		IssuedAt:   time.Now().Truncate(time.Second),
	}
	fillSongInfo(c, t, request)

	// 指纹和绘制都在扣费之前完成：音频取不到、字体没配好时不收钱
	if c.AudioSHA256, c.AudioSize, err = s.fingerprint(ctx, t); err != nil {
		return nil, err
	}
	c.No = newCertificateNo(c.IssuedAt)
	if _, err := s.PDF(c); err != nil {
		return nil, err
	}

	remark := truncateRunes("创作证明：《"+c.Title+"》", 120)
	for attempt := 0; ; attempt++ {
		cert, existing, balance, err := s.store.IssueCertificate(ctx, c, remark)
		switch {
		case errors.Is(err, storage.ErrCertificateNoTaken) && attempt < 3:
			c.No = newCertificateNo(c.IssuedAt)
			continue
		case errors.Is(err, storage.ErrInsufficientPoints):
			return nil, httpx.NoPoints(fmt.Sprintf("积分不足，创作证明需要 %d 积分，当前余额 %d", c.PointsCost, balance))
		case err != nil:
			return nil, err
		}
		if !existing {
			log.Printf("[certificate] 签发 %s task=%d merchant=%d", cert.No, cert.TaskID, cert.MerchantID)
		}
		return &Result{Certificate: cert, Charged: !existing && cert.PointsCost > 0, Balance: balance}, nil
	}
}

// SongTaskID 按 custom_id 找到商户的作品任务。
func (s *Service) SongTaskID(ctx context.Context, merchantID int64, sunoID string) (int64, error) {
	id, err := s.store.SongTaskByCustomID(ctx, merchantID, strings.TrimSpace(sunoID))
	if errors.Is(err, storage.ErrNotFound) {
		return 0, httpx.NotFound("作品不存在或尚未生成完成")
	}
	return id, err
}

// Get 按编号读取证明；merchantID 大于 0 时只能读自己的。
func (s *Service) Get(ctx context.Context, merchantID int64, no string) (*model.Certificate, error) {
	c, err := s.store.CertificateByNo(ctx, strings.ToUpper(strings.TrimSpace(no)))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, httpx.NotFound("创作证明不存在")
		}
		return nil, err
	}
	if merchantID > 0 && c.MerchantID != merchantID {
		return nil, httpx.NotFound("创作证明不存在")
	}
	return c, nil
}

// ByTask 查询作品已签发的证明，未签发返回 nil。
func (s *Service) ByTask(ctx context.Context, taskID int64) (*model.Certificate, error) {
	c, err := s.store.CertificateByTask(ctx, taskID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	return c, err
}

// Public 公开核验时展示的字段：不含歌词与商户信息。
func (s *Service) Public(c *model.Certificate) map[string]interface{} {
	return map[string]interface{}{
		"certificate_no":  c.No,
		"title":           c.Title,
		"author":          c.Author,
		"suno_id":         c.SunoID,
		"duration":        c.Duration,
		"audio_sha256":    c.AudioSHA256,
		"song_created_at": c.SongCreatedAt,
		"issued_at":       c.IssuedAt,
		"issuer":          s.opts.Issuer,
	}
}

// VerifyLink 证书的在线核验地址，未配置核验页时为空。
func (s *Service) VerifyLink(no string) string {
	base := strings.TrimSpace(s.opts.VerifyURL)
	if base == "" {
		return ""
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "no=" + url.QueryEscape(no)
}

// FileName 下载时的文件名。
func FileName(c *model.Certificate) string {
	title := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) || r < 0x20 {
			return -1
		}
		return r
	}, c.Title)
	return fmt.Sprintf("创作证明-%s-%s.pdf", truncateRunes(title, 40), c.No)
}

// checkSong 只有已完成、带 custom_id 的歌曲才能签发证明。
func checkSong(t *model.Task) error {
	switch t.Kind {
	case model.KindVideo, model.KindAlignedLyrics, model.KindVoiceTrain:
		return httpx.BadRequest("该任务不是音乐作品，无法签发创作证明")
	}
	if t.Status != model.StatusCompleted || t.CustomID == nil || *t.CustomID == "" {
		return httpx.BadRequest("作品尚未生成完成，暂时无法签发创作证明")
	}
	return nil
}

// fillSongInfo 从上游返回的歌曲信息里取标题、歌词、风格与模型，缺失时回退到提交时的参数。
func fillSongInfo(c *model.Certificate, t *model.Task, request string) {
	var clips []struct {
		ID                string `json:"id"`
		Title             string `json:"title"`
		ModelName         string `json:"model_name"`
		MajorModelVersion string `json:"major_model_version"`
		Metadata          struct {
			Prompt           string  `json:"prompt"`
			Tags             string  `json:"tags"`
			Duration         float64 `json:"duration"`
			MakeInstrumental bool    `json:"make_instrumental"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(t.Extend), &clips) == nil {
		for _, clip := range clips {
			if clip.ID != "" && clip.ID != c.SunoID && len(clips) > 1 {
				continue
			}
			c.Title = clip.Title
			c.Tags = clip.Metadata.Tags
			c.Duration = clip.Metadata.Duration
			if !clip.Metadata.MakeInstrumental {
				c.Lyrics = clip.Metadata.Prompt
			}
			c.ModelName = clip.MajorModelVersion
			if c.ModelName == "" {
				c.ModelName = clip.ModelName
			}
			break
		}
	}

	var req struct {
		Title            string `json:"title"`
		Prompt           string `json:"prompt"`
		Tags             string `json:"tags"`
		MV               string `json:"mv"`
		MakeInstrumental bool   `json:"make_instrumental"`
	}
	_ = json.Unmarshal([]byte(request), &req)
	if strings.TrimSpace(c.Title) == "" {
		c.Title = req.Title
	}
	if strings.TrimSpace(c.Title) == "" {
		c.Title = "未命名作品"
	}
	if c.Lyrics == "" && !req.MakeInstrumental {
		c.Lyrics = req.Prompt
	}
	if c.Tags == "" {
		c.Tags = req.Tags
	}
	if c.ModelName == "" {
		c.ModelName = req.MV
	}
	if c.Duration == 0 && t.FileInfo != nil {
		c.Duration = t.FileInfo.Duration
	}

	c.Title = truncateRunes(strings.TrimSpace(c.Title), 200)
	c.Tags = truncateRunes(strings.TrimSpace(c.Tags), 1000)
	c.ModelName = truncateRunes(c.ModelName, 64)
	c.Lyrics = strings.TrimSpace(c.Lyrics)

	c.SongCreatedAt = t.CreatedAt
	if t.FinishedAt != nil {
		c.SongCreatedAt = *t.FinishedAt
	}
}

// fingerprint 计算作品 MP3 文件的 SHA-256。用的是后台「下载 MP3」给出的同一个文件，
// 用户拿下载到的文件去核验页比对时指纹能对上。
func (s *Service) fingerprint(ctx context.Context, t *model.Task) (string, int64, error) {
	if s.opts.MP3 == nil {
		return "", 0, errors.New("未配置 MP3 生成器")
	}
	data, err := s.opts.MP3(ctx, t)
	if err != nil {
		if s.opts.MockAudio {
			if c := media.AudioCandidates(t); len(c) > 0 {
				sum := sha256.Sum256([]byte(c[0]))
				return hex.EncodeToString(sum[:]), 0, nil
			}
		}
		if errors.Is(err, archive.ErrNoAudio) {
			return "", 0, httpx.BadRequest("作品音频已失效，无法生成创作证明")
		}
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), int64(len(data)), nil
}

// 证书编号：SC + 签发日期 + 8 位随机码，去掉易混淆的 0/O/1/I，外人难以枚举。
const noAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

func newCertificateNo(at time.Time) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = noAlphabet[int(b[i])%len(noAlphabet)]
	}
	return "SC" + at.Format("20060102") + "-" + string(b)
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
