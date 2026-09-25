package adminapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lepro/suno-open-api/internal/archive"
	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/storage"
)

// SetMP3 设置作品 MP3 生成器。
func (s *Server) SetMP3(m *archive.MP3Maker) { s.mp3 = m }

// songMP3 以 MP3 格式下载作品音频，文件名为歌名。
func (s *Server) songMP3(w http.ResponseWriter, r *http.Request) error {
	if s.mp3 == nil {
		return httpx.Internal("MP3 下载未启用")
	}
	taskID, err := httpx.QueryInt64(r, "task_id")
	if err != nil {
		return err
	}
	row, request, err := s.store.AdminTaskByID(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("作品不存在")
		}
		return err
	}
	t := &row.Task
	if t.Status != model.StatusCompleted || t.Kind == model.KindVideo {
		return httpx.BadRequest("作品尚未生成完成，暂时无法下载")
	}

	data, err := s.mp3.Get(r.Context(), t)
	if err != nil {
		if errors.Is(err, archive.ErrNoAudio) {
			return httpx.BadRequest("作品音频已失效，无法下载")
		}
		return err
	}

	name := songFileName(request, t.ID) + ".mp3"
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="song-%d.mp3"; filename*=UTF-8''%s`, t.ID, url.PathEscape(name)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data)
	return nil
}

// songFileName 用提交时的歌名作文件名，去掉文件系统不允许的字符；没有歌名时用任务编号。
func songFileName(request string, taskID int64) string {
	var req struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal([]byte(request), &req)
	title := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) || r < 0x20 {
			return -1
		}
		return r
	}, strings.TrimSpace(req.Title))
	if runes := []rune(title); len(runes) > 60 {
		title = string(runes[:60])
	}
	if title == "" {
		return fmt.Sprintf("作品-%d", taskID)
	}
	return title
}
