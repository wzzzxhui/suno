package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

// Suno 对接真实上游接口。配置 SUNO_PROVIDER=suno 后启用。
type Suno struct {
	base   string
	key    string
	client *http.Client
}

// NewSuno 创建上游客户端。
func NewSuno(base, key string, timeout time.Duration) *Suno {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Suno{
		base:   strings.TrimRight(base, "/"),
		key:    key,
		client: &http.Client{Timeout: timeout},
	}
}

// Name 实现 Provider。
func (s *Suno) Name() string { return "suno" }

// Balance 查询上游账户余额。该接口免费，可放心定时调用。
func (s *Suno) Balance(ctx context.Context) (int64, error) {
	raw, err := s.do(ctx, http.MethodGet, "/api/v1/points/balance", nil, nil)
	if err != nil {
		return 0, err
	}

	var payload struct {
		RemainingPoints interface{} `json:"remaining_points"`
	}
	if err := json.Unmarshal(raw.RawData, &payload); err != nil {
		return 0, fmt.Errorf("解析上游余额失败: %w", err)
	}
	return int64(toFloat(payload.RemainingPoints)), nil
}

// endpointOf 把任务类型映射到上游提交路径。
// 图片与视频类走能力注册表，音乐类保持原有映射。
func endpointOf(kind model.TaskKind) (string, error) {
	if c, ok := model.CapabilityOf(kind); ok {
		return c.SubmitPath, nil
	}
	switch kind {
	case model.KindGenerate:
		return "/api/v1/music/generate", nil
	case model.KindSound:
		return "/api/v1/music/sound", nil
	case model.KindUpload:
		return "/api/v1/music/upload", nil
	case model.KindWholeSong:
		return "/api/v1/music/whole-song", nil
	case model.KindAlignedLyrics:
		return "/api/v1/music/aligned-lyrics", nil
	case model.KindUpsample:
		return "/api/v1/music/upsample", nil
	case model.KindVideo:
		return "/api/v1/music/video", nil
	case model.KindCrop:
		return "/api/v1/music/crop", nil
	case model.KindSpeed:
		return "/api/v1/music/speed", nil
	case model.KindDownloadWAV:
		return "/api/v2/music/download-wav", nil
	case model.KindDownloadMP3:
		return "/api/v2/music/download-mp3", nil
	case model.KindDownloadM4A:
		return "/api/v2/music/download-m4a", nil
	default:
		return "", fmt.Errorf("未知任务类型: %s", kind)
	}
}

// Submit 提交任务到上游并解析出任务 ID。
func (s *Suno) Submit(ctx context.Context, req *SubmitRequest) (*SubmitResult, error) {
	endpoint, err := endpointOf(req.Kind)
	if err != nil {
		return nil, err
	}

	payload := withoutInternalKeys(req.Payload)
	if req.Kind == model.KindVideoSeedance {
		payload = seedancePayload(payload)
	}

	raw, err := s.do(ctx, http.MethodPost, endpoint, payload, nil)
	if err != nil {
		return nil, err
	}

	ids := extractTaskIDs(raw.Data)
	if len(ids) == 0 {
		return nil, fmt.Errorf("上游未返回任务 ID: %s", string(raw.RawData))
	}
	return &SubmitResult{ProviderIDs: ids}, nil
}

// Fetch 查询上游任务状态。
func (s *Suno) Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error) {
	// 音乐走 /music/task?id=，图片与视频各有自己的查询路径且参数名为 task_id
	path, query := "/api/v1/music/task", map[string]string{"id": req.ProviderID}
	if c, ok := model.CapabilityOf(req.Kind); ok && c.QueryPath != "" {
		path, query = c.QueryPath, map[string]string{"task_id": req.ProviderID}
	}

	raw, err := s.do(ctx, http.MethodGet, path, nil, query)
	if err != nil {
		return nil, err
	}

	switch model.GroupOf(req.Kind) {
	case model.GroupImage:
		return parseImageTask(raw.RawData)
	case model.GroupVideo:
		return parseVideoTask(raw.RawData)
	}

	// 上游把任务壳与歌曲数据分了两层：状态在 data 上，产出在 data.result 里。
	var payload struct {
		Status interface{}   `json:"status"`
		Error  interface{}   `json:"error"`
		Result *upstreamClip `json:"result"`
		// 少数接口会把产出直接平铺在 data 上，这里一并兜住
		upstreamClip
	}
	if err := json.Unmarshal(raw.RawData, &payload); err != nil {
		return nil, fmt.Errorf("解析上游任务结果失败: %w", err)
	}

	clip := payload.Result
	if clip == nil {
		clip = &payload.upstreamClip
	}

	// 状态优先取外层字符串，缺失时回落到 result 里的数字状态
	status := normalizeStatus(payload.Status)
	if payload.Status == nil && clip != nil {
		status = normalizeStatus(clip.Status)
	}

	result := &FetchResult{
		Status:   status,
		ProxyURL: clip.ProxyURL,
		Reason:   firstNonEmpty(asString(payload.Error), asString(clip.ErrorMsg)),
	}
	if clip.CustomID != nil {
		result.CustomID = *clip.CustomID
	}

	switch v := clip.Extend.(type) {
	case string:
		result.Extend = v
	case nil:
		// 没有 extend，忽略
	default:
		if b, err := json.Marshal(v); err == nil {
			result.Extend = string(b)
		}
	}

	if clip.FileInfo != nil {
		fi := clip.FileInfo
		// 封面在不同任务里分别落在 coverUrl / cosUrl / bigUrl 上
		cover := firstNonEmpty(fi.CoverURL, fi.CosURL, fi.BigURL, fi.ThumbImg)
		result.FileInfo = &model.FileInfo{
			MP3URL:   fi.MP3URL,
			MP4URL:   fi.MP4URL,
			WAVURL:   fi.WAVURL,
			M4AURL:   fi.M4AURL,
			CoverURL: cover,
			Duration: toFloat(fi.Duration),
		}
	}
	return result, nil
}

// parseImageTask 解析绘画任务：结果是 result.images 数组。
func parseImageTask(raw []byte) (*FetchResult, error) {
	var payload struct {
		TaskID   string      `json:"task_id"`
		TaskType string      `json:"task_type"`
		Status   interface{} `json:"status"`
		ErrorMsg interface{} `json:"error_msg"`
		Result   *struct {
			Images []string `json:"images"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析绘画任务失败: %w", err)
	}

	result := &FetchResult{
		Status: normalizeStatus(payload.Status),
		Reason: asString(payload.ErrorMsg),
	}
	if payload.Result != nil && len(payload.Result.Images) > 0 {
		result.FileInfo = &model.FileInfo{
			Images:   payload.Result.Images,
			CoverURL: payload.Result.Images[0],
		}
		if b, err := json.Marshal(payload.Result); err == nil {
			result.Extend = string(b)
		}
	}
	return result, nil
}

// parseVideoTask 解析视频任务：结果是 video_url，另有签名代理地址。
func parseVideoTask(raw []byte) (*FetchResult, error) {
	var payload struct {
		TaskID   string      `json:"task_id"`
		Status   interface{} `json:"status"`
		VideoURL string      `json:"video_url"`
		ProxyURL string      `json:"proxy_url"`
		ErrorMsg interface{} `json:"error_msg"`
		Error    interface{} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析视频任务失败: %w", err)
	}

	result := &FetchResult{
		Status:   normalizeStatus(payload.Status),
		ProxyURL: payload.ProxyURL,
		Reason:   firstNonEmpty(asString(payload.ErrorMsg), asString(payload.Error)),
	}
	if payload.VideoURL != "" {
		result.FileInfo = &model.FileInfo{MP4URL: payload.VideoURL}
		if b, err := json.Marshal(map[string]string{"video_url": payload.VideoURL}); err == nil {
			result.Extend = string(b)
		}
	}
	return result, nil
}

// upstreamClip 是上游返回的歌曲/产出数据。
type upstreamClip struct {
	Status   interface{} `json:"status"`
	CustomID *string     `json:"custom_id"`
	ProxyURL string      `json:"proxy_url"`
	Extend   interface{} `json:"extend"`
	ErrorMsg interface{} `json:"errormsg"`
	FileInfo *struct {
		MP3URL   string      `json:"mp3Url"`
		MP4URL   string      `json:"mp4Url"`
		WAVURL   string      `json:"wavUrl"`
		M4AURL   string      `json:"m4aUrl"`
		CoverURL string      `json:"coverUrl"`
		CosURL   string      `json:"cosUrl"`
		BigURL   string      `json:"bigUrl"`
		ThumbImg string      `json:"thumbImg"`
		Duration interface{} `json:"duration"`
	} `json:"fileInfo"`
}

// asString 把上游可能是字符串或 null 的字段转成文本。
func asString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

type upstreamResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    interface{}     `json:"-"`
	RawData json.RawMessage `json:"data"`
}

func (s *Suno) do(ctx context.Context, method, path string, body map[string]interface{}, query map[string]string) (*upstreamResponse, error) {
	url := s.base + path
	if len(query) > 0 {
		parts := make([]string, 0, len(query))
		for k, v := range query {
			parts = append(parts, k+"="+v)
		}
		url += "?" + strings.Join(parts, "&")
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求上游失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("上游返回 HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var parsed upstreamResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("上游响应不是合法 JSON: %s", truncate(string(raw), 300))
	}
	if parsed.Code != 0 && parsed.Code != http.StatusOK {
		return nil, fmt.Errorf("上游返回错误 %d: %s", parsed.Code, parsed.Message)
	}

	_ = json.Unmarshal(parsed.RawData, &parsed.Data)
	return &parsed, nil
}

// extractTaskIDs 兼容上游的多种返回形态：
// data 为数字、数字数组、{task_id}、{task_ids: []}。
func extractTaskIDs(data interface{}) []string {
	switch v := data.(type) {
	case float64:
		return []string{trimFloat(v)}
	case string:
		if v != "" {
			return []string{v}
		}
	case []interface{}:
		ids := make([]string, 0, len(v))
		for _, item := range v {
			ids = append(ids, extractTaskIDs(item)...)
		}
		return ids
	case map[string]interface{}:
		if raw, ok := v["task_ids"]; ok {
			return extractTaskIDs(raw)
		}
		if raw, ok := v["task_id"]; ok {
			return extractTaskIDs(raw)
		}
		if raw, ok := v["id"]; ok {
			return extractTaskIDs(raw)
		}
	}
	return nil
}

func normalizeStatus(raw interface{}) model.TaskStatus {
	switch v := raw.(type) {
	case string:
		switch strings.ToLower(v) {
		case "completed", "complete", "success":
			return model.StatusCompleted
		case "failed", "fail", "error":
			return model.StatusFailed
		case "processing", "running":
			return model.StatusProcessing
		default:
			return model.StatusPending
		}
	case float64:
		switch int(v) {
		case 3:
			return model.StatusCompleted
		case 4:
			return model.StatusFailed
		case 2:
			return model.StatusProcessing
		}
	}
	return model.StatusPending
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%f", &f); err == nil {
			return f
		}
	}
	return 0
}

func trimFloat(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.0f", v), "0"), ".")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// seedancePayload 把平铺的 Seedance 参数转换成上游要求的 content 数组：
// 画面比例、分辨率、时长以 --ratio / --rs / --dur 追加在提示词末尾，参考图作为 image_url 元素。
func seedancePayload(in map[string]interface{}) map[string]interface{} {
	if _, ok := in["content"]; ok {
		return in // 调用方已按上游格式传入
	}

	text := strings.TrimSpace(asString(in["prompt"]))
	for _, f := range []struct{ key, flag string }{{"ratio", "--ratio"}, {"resolution", "--rs"}, {"duration", "--dur"}} {
		switch v := in[f.key].(type) {
		case nil:
		case float64:
			text += fmt.Sprintf(" %s %s", f.flag, trimFloat(v))
		case int:
			text += fmt.Sprintf(" %s %d", f.flag, v)
		default:
			if sv := strings.TrimSpace(asString(v)); sv != "" {
				text += fmt.Sprintf(" %s %s", f.flag, sv)
			}
		}
	}

	content := []map[string]interface{}{{"type": "text", "text": strings.TrimSpace(text)}}
	var images []string
	switch v := in["reference_images"].(type) {
	case []string:
		images = v
	case []interface{}:
		for _, item := range v {
			if u := strings.TrimSpace(asString(item)); u != "" {
				images = append(images, u)
			}
		}
	}
	for _, u := range images {
		content = append(content, map[string]interface{}{"type": "image_url", "image_url": map[string]string{"url": u}})
	}

	out := map[string]interface{}{"content": content}
	if m := strings.TrimSpace(asString(in["model"])); m != "" {
		out["model"] = m
	} else {
		out["model"] = "doubao-seedance-1-0-pro-fast-251015"
	}
	return out
}

// internalKeys 只在本平台内部使用的请求字段（如创作时选的音色），不发给上游。
var internalKeys = []string{"voice"}

func withoutInternalKeys(payload map[string]interface{}) map[string]interface{} {
	has := false
	for _, k := range internalKeys {
		if _, ok := payload[k]; ok {
			has = true
		}
	}
	if !has {
		return payload
	}
	out := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		out[k] = v
	}
	for _, k := range internalKeys {
		delete(out, k)
	}
	return out
}
