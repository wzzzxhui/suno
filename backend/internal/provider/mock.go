package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

// Mock 是本地模拟上游：提交后延迟一段时间即产出可用的假数据，
// 用于前端联调与演示，不访问任何外部服务。
type Mock struct {
	cdnBase string
	delay   time.Duration

	mu    sync.RWMutex
	tasks map[string]*mockTask
}

type mockTask struct {
	kind     model.TaskKind
	customID string
	title    string
	readyAt  time.Time
	payload  map[string]interface{}
}

// NewMock 创建模拟 Provider。
func NewMock(cdnBase string, delay time.Duration) *Mock {
	if delay <= 0 {
		delay = 10 * time.Second
	}
	return &Mock{
		cdnBase: strings.TrimRight(cdnBase, "/"),
		delay:   delay,
		tasks:   make(map[string]*mockTask),
	}
}

// Name 实现 Provider。
func (m *Mock) Name() string { return "mock" }

// Submit 登记一个模拟任务。
func (m *Mock) Submit(ctx context.Context, req *SubmitRequest) (*SubmitResult, error) {
	count := OutputCount(req.Kind)
	ids := make([]string, 0, count)

	title, _ := req.Payload["title"].(string)
	if title == "" {
		title = "Untitled"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for i := 0; i < count; i++ {
		providerID := "mock_" + randomHex(8)
		m.tasks[providerID] = &mockTask{
			kind:     req.Kind,
			customID: randomUUID(),
			title:    title,
			readyAt:  time.Now().Add(m.delay),
			payload:  req.Payload,
		}
		ids = append(ids, providerID)
	}
	return &SubmitResult{ProviderIDs: ids}, nil
}

// Fetch 返回模拟任务的状态：到点之前是 processing，之后是 completed。
func (m *Mock) Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error) {
	m.mu.RLock()
	task, ok := m.tasks[req.ProviderID]
	m.mu.RUnlock()

	if !ok {
		return &FetchResult{Status: model.StatusFailed, Reason: "模拟任务不存在"}, nil
	}
	if time.Now().Before(task.readyAt) {
		return &FetchResult{Status: model.StatusProcessing}, nil
	}

	base := fmt.Sprintf("%s/%s", m.cdnBase, task.customID)
	result := &FetchResult{
		Status:   model.StatusCompleted,
		CustomID: task.customID,
	}

	switch task.kind {
	case model.KindVideo:
		result.CustomID = ""
		result.ProxyURL = base + ".mp4"
		result.FileInfo = &model.FileInfo{MP4URL: base + ".mp4"}
		result.Extend = mustJSON(map[string]interface{}{
			"status":    "complete",
			"video_url": base + ".mp4",
		})

	case model.KindVoiceTrain:
		result.CustomID = ""
		result.Extend = mustJSON(map[string]interface{}{
			"state": 3,
			"outputs": []interface{}{map[string]interface{}{"smartContentResult": map[string]interface{}{
				"singingCloning": map[string]interface{}{"modelName": task.payload["model_name"]}}}},
		})

	case model.KindVoiceCover:
		result.CustomID = ""
		result.ProxyURL = base + ".mp3"
		result.FileInfo = &model.FileInfo{MP3URL: base + ".mp3"}
		result.Extend = mustJSON(map[string]interface{}{"state": 3, "songName": "cover.mp3"})

	case model.KindAlignedLyrics:
		lyrics, _ := task.payload["lyrics"].(string)
		result.Extend = mustJSON(map[string]interface{}{
			"alignment": buildMockAlignment(lyrics),
		})

	case model.KindDownloadWAV:
		result.FileInfo = &model.FileInfo{WAVURL: base + ".wav"}
		result.Extend = mustJSON(map[string]interface{}{"audio_url": base + ".wav", "format": "wav"})

	case model.KindDownloadM4A:
		result.FileInfo = &model.FileInfo{M4AURL: base + ".m4a"}
		result.Extend = mustJSON(map[string]interface{}{"audio_url": base + ".m4a", "format": "m4a"})

	default:
		// 图像视频模块：图片能力产出图片，视频能力产出 mp4
		if model.GroupOf(task.kind) == model.GroupImage {
			result.CustomID = ""
			result.FileInfo = &model.FileInfo{Images: []string{base + ".png"}, CoverURL: base + ".png"}
			break
		}
		if model.GroupOf(task.kind) == model.GroupVideo {
			result.CustomID = ""
			result.FileInfo = &model.FileInfo{MP4URL: base + ".mp4"}
			result.Extend = mustJSON(map[string]interface{}{"video_url": base + ".mp4"})
			break
		}
		result.FileInfo = &model.FileInfo{
			MP3URL:   base + ".mp3",
			CoverURL: base + ".png",
			Duration: 128.5,
		}
		result.Extend = mustJSON([]map[string]interface{}{{
			"id":           task.customID,
			"title":        task.title,
			"audio_url":    base + ".mp3",
			"image_url":    base + ".png",
			"duration":     128.5,
			"model_name":   valueOrDefault(task.payload, "mv", "chirp-hawk"),
			"tags":         valueOrDefault(task.payload, "tags", ""),
			"prompt":       valueOrDefault(task.payload, "prompt", ""),
			"instrumental": task.payload["make_instrumental"] == true,
		}})
	}

	return result, nil
}

// buildMockAlignment 按词切分歌词，生成递增的时间戳。
func buildMockAlignment(lyrics string) []map[string]interface{} {
	fields := strings.Fields(lyrics)
	if len(fields) == 0 {
		fields = []string{"la", "la", "la"}
	}
	out := make([]map[string]interface{}, 0, len(fields))
	cursor := 0.0
	for _, word := range fields {
		start := cursor
		cursor += 0.4
		out = append(out, map[string]interface{}{
			"word":    word,
			"start_s": round2(start),
			"end_s":   round2(cursor),
			"success": true,
			"p_align": 1,
		})
	}
	return out
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func valueOrDefault(payload map[string]interface{}, key, fallback string) string {
	if v, ok := payload[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func mustJSON(v interface{}) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// randomUUID 生成 v4 形式的 UUID，避免为此引入第三方依赖。
func randomUUID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return randomHex(16)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
