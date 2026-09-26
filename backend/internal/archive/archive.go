// Package archive 把生成完成的作品（音频与封面）转存到平台自己的 COS。
//
// 上游返回的音频地址约一天后就会被清理，转存后播放、做 MV、翻唱都从平台副本读取。
package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/media"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/provider"
	"github.com/lepro/suno-open-api/internal/storage"
)

const maxFileSize = 100 << 20

// Kinds 会产出歌曲音频、需要转存的任务类型。音色翻唱的结果本就在平台 COS，不在此列。
var Kinds = []model.TaskKind{
	model.KindGenerate, model.KindSound, model.KindUpload, model.KindWholeSong, model.KindUpsample,
	model.KindCrop, model.KindSpeed, model.KindDownloadWAV, model.KindDownloadMP3, model.KindDownloadM4A,
	model.KindVoiceSong,
}

var archivable = func() map[model.TaskKind]bool {
	m := make(map[model.TaskKind]bool, len(Kinds))
	for _, k := range Kinds {
		m[k] = true
	}
	return m
}()

// Archiver 转存器。
type Archiver struct {
	store    *storage.Store
	provider provider.Provider // 上游副本失效时调「下载 MP3」重新获取
	signer   *provider.COSSigner
	base     string
	client   *http.Client
	slots    chan struct{} // 限制同时转存的数量，避免占满带宽
}

// New 创建转存器；未配置 COS 时返回 nil。p 用于在上游副本失效时重新下载，可为 nil。
func New(store *storage.Store, p provider.Provider, signer *provider.COSSigner, base string) *Archiver {
	if !signer.Enabled() || base == "" {
		return nil
	}
	return &Archiver{
		store:    store,
		provider: p,
		signer:   signer,
		base:     strings.TrimRight(base, "/"),
		client:   &http.Client{Timeout: 3 * time.Minute},
		slots:    make(chan struct{}, 4),
	}
}

// Sign 把 COS 对象路径签成可访问地址，供读取作品时替换上游地址。
func (a *Archiver) Sign(key string, expire time.Duration) string {
	s := *a.signer
	s.Expire = expire
	u, err := s.Sign(a.base+"/"+key, time.Now())
	if err != nil {
		return ""
	}
	return u
}

// Async 在后台转存一个任务，不阻塞调用方。
func (a *Archiver) Async(ctx context.Context, taskID int64) {
	if a == nil {
		return
	}
	go func() {
		a.slots <- struct{}{}
		defer func() { <-a.slots }()
		if err := a.Archive(ctx, taskID); err != nil {
			log.Printf("[archive] 转存失败 task=%d: %v", taskID, err)
		}
	}()
}

// Archive 转存一个任务的音频与封面；非音频任务或已转存时直接返回。
func (a *Archiver) Archive(ctx context.Context, taskID int64) error {
	row, _, err := a.store.AdminTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	t := &row.Task
	if !archivable[t.Kind] || t.Status != model.StatusCompleted {
		return nil
	}
	if t.FileInfo != nil && t.FileInfo.Archived != nil {
		return nil
	}

	var out model.ArchivedFiles
	prefix := fmt.Sprintf("songs/m%d/%d", t.MerchantID, t.ID)

	audio := media.PlayableAudio(ctx, t)
	if audio == "" {
		// 上游副本已被清理（Suno CDN 上的副本是加密的，不能用），让上游重新导出一份 MP3
		if audio, err = a.redownload(ctx, t); err != nil {
			log.Printf("[archive] 重新下载失败 task=%d: %v", t.ID, err)
			out.Missing = true
			return a.store.SetArchived(ctx, t.ID, out)
		}
	}
	if out.Audio, err = a.copy(ctx, audio, prefix, ".mp3", true); err != nil {
		return fmt.Errorf("转存音频: %w", err)
	}

	// 封面失败不影响音频转存
	if cover := coverOf(t); cover != "" {
		if key, err := a.copy(ctx, cover, prefix+"_cover", ".jpg", false); err == nil {
			out.Cover = key
		} else {
			log.Printf("[archive] 封面转存失败 task=%d: %v", t.ID, err)
		}
	}
	if err := a.store.SetArchived(ctx, t.ID, out); err != nil {
		return err
	}
	log.Printf("[archive] 已转存 task=%d %s", t.ID, out.Audio)
	return nil
}

// Backfill 转存历史作品：上游副本仍可访问的补存到 COS。启动时与之后每小时执行一次。
func (a *Archiver) Backfill(ctx context.Context) {
	if a == nil {
		return
	}
	run := func() {
		ids, err := a.store.UnarchivedSongs(ctx, Kinds, 200)
		if err != nil {
			log.Printf("[archive] 查询待转存作品失败: %v", err)
			return
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			if err := a.Archive(ctx, id); err != nil {
				log.Printf("[archive] 补转存失败 task=%d: %v", id, err)
			}
		}
	}
	run()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// Delete 删除作品在 COS 中的副本。
func (a *Archiver) Delete(ctx context.Context, files *model.ArchivedFiles) {
	if a == nil || files == nil {
		return
	}
	for _, key := range []string{files.Audio, files.Cover, files.MP3} {
		if key == "" || (key == files.MP3 && key == files.Audio) {
			continue
		}
		if err := a.signer.Delete(ctx, a.client, a.base+"/"+key); err != nil {
			log.Printf("[archive] 删除副本失败 %s: %v", key, err)
		}
	}
}

// redownload 调上游「下载 MP3」从 Suno 重新导出一份音频，返回下载地址。
func (a *Archiver) redownload(ctx context.Context, t *model.Task) (string, error) {
	if a.provider == nil || t.CustomID == nil || *t.CustomID == "" {
		return "", fmt.Errorf("没有可重新下载的 Suno ID")
	}
	res, err := a.provider.Submit(ctx, &provider.SubmitRequest{
		Kind: model.KindDownloadMP3, Payload: map[string]interface{}{"suno_id": *t.CustomID},
	})
	if err != nil {
		return "", err
	}
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
		r, err := a.provider.Fetch(ctx, &provider.FetchRequest{Kind: model.KindDownloadMP3, ProviderID: res.ProviderIDs[0]})
		if err != nil {
			continue
		}
		switch r.Status {
		case model.StatusCompleted:
			var out struct {
				DownloadURL string `json:"download_url"`
				AudioURL    string `json:"audio_url"`
			}
			_ = json.Unmarshal([]byte(r.Extend), &out)
			if u := media.FirstPlayable(ctx, []string{out.DownloadURL, out.AudioURL, r.ProxyURL}); u != "" {
				return u, nil
			}
			return "", fmt.Errorf("上游导出完成但文件不可用")
		case model.StatusFailed:
			return "", fmt.Errorf("上游导出失败：%s", r.Reason)
		}
	}
	return "", fmt.Errorf("上游导出超时")
}

// copy 下载 src 并写入 COS，返回对象路径；audio 为 true 时校验文件头确实是音频。
func (a *Archiver) copy(ctx context.Context, src, prefix, fallbackExt string, audio bool) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileSize+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > maxFileSize {
		return "", fmt.Errorf("文件为空或超过 100MB")
	}
	if audio && !media.IsAudio(data[:min(len(data), 16)]) {
		return "", fmt.Errorf("下载的内容不是可播放的音频")
	}

	ext := extOf(src, fallbackExt)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || strings.HasPrefix(contentType, "application/octet-stream") {
		contentType = map[string]string{
			".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav",
			".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp",
		}[ext]
	}
	key := prefix + ext
	if err := a.signer.Put(ctx, a.client, a.base+"/"+key, contentType, data); err != nil {
		return "", err
	}
	return key, nil
}

// coverOf 取作品封面：优先大图。
func coverOf(t *model.Task) string {
	var clips []struct {
		ID            string `json:"id"`
		ImageURL      string `json:"image_url"`
		ImageLargeURL string `json:"image_large_url"`
	}
	if json.Unmarshal([]byte(t.Extend), &clips) == nil {
		for _, c := range clips {
			if t.CustomID != nil && c.ID != "" && c.ID != *t.CustomID && len(clips) > 1 {
				continue
			}
			if u := firstNonEmpty(c.ImageLargeURL, c.ImageURL); u != "" {
				return u
			}
		}
	}
	if t.FileInfo != nil {
		return t.FileInfo.CoverURL
	}
	return ""
}

func extOf(u, fallback string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	if ext := strings.ToLower(filepath.Ext(u)); ext != "" && len(ext) <= 5 {
		return ext
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
