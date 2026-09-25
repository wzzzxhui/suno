// Package media 为作品挑选当前仍可播放的音频地址。
//
// 上游转存的音频（fileInfo 里的地址）约一天后就会被清理；
// Suno 自己 CDN 上的副本（extend 里的 media_urls）虽然能访问，但内容是加密的，无法播放。
// 因此挑选音频时不能只看能否访问，还要检查文件头确认是真正的音频。
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

// AudioCandidates 按优先级列出作品的全部音频地址（去重）。
func AudioCandidates(t *model.Task) []string {
	var out []string
	seen := map[string]bool{}
	add := func(u string) {
		if u = strings.TrimSpace(u); u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}

	if f := t.FileInfo; f != nil {
		add(f.MP3URL)
		add(f.M4AURL)
		add(f.WAVURL)
	}

	var clips []struct {
		ID        string `json:"id"`
		AudioURL  string `json:"audio_url"`
		MediaURLs []struct {
			URL string `json:"url"`
		} `json:"media_urls"`
	}
	if json.Unmarshal([]byte(t.Extend), &clips) == nil {
		for _, c := range clips {
			if t.CustomID != nil && c.ID != "" && c.ID != *t.CustomID && len(clips) > 1 {
				continue
			}
			add(c.AudioURL)
			for _, m := range c.MediaURLs {
				add(m.URL)
			}
		}
	}
	return out
}

// IsAudio 按文件头判断是否为可播放的音频：MP3、MP4/M4A、WAV、OGG、FLAC、AAC。
func IsAudio(head []byte) bool {
	switch {
	case len(head) >= 3 && bytes.Equal(head[:3], []byte("ID3")):
		return true
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0: // MP3 / AAC 帧同步
		return true
	case len(head) >= 8 && bytes.Equal(head[4:8], []byte("ftyp")):
		return true
	case len(head) >= 4 && (bytes.Equal(head[:4], []byte("RIFF")) || bytes.Equal(head[:4], []byte("OggS")) ||
		bytes.Equal(head[:4], []byte("fLaC"))):
		return true
	}
	return false
}

// FirstPlayable 返回第一个能下载且确实是音频的地址；都不可用时返回空串。
// 部分 CDN 不支持 HEAD，这里用只取前 16 字节的 GET 探测。
func FirstPlayable(ctx context.Context, urls []string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Range", "bytes=0-15")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		head, _ := io.ReadAll(io.LimitReader(resp.Body, 16))
		resp.Body.Close()
		if (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) && IsAudio(head) {
			return u
		}
	}
	return ""
}

// PlayableAudio 为作品挑一个当前可播放的音频地址。
func PlayableAudio(ctx context.Context, t *model.Task) string {
	return FirstPlayable(ctx, AudioCandidates(t))
}
