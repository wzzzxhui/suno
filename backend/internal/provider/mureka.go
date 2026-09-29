package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

// Mureka（昆仑万维）是目前唯一在公开 API 里支持「用上传的人声直接生成歌曲」的上游：
// 先用 song/vocal-clone 上传 15~30 秒清唱拿到 vocal_id，之后每次 song/generate 带上 vocal_id，
// 生成的歌从一开始就是这个声音在唱，不经过先生成再翻唱。
// 文档：https://platform.mureka.cn/docs/

// VocalService 演唱音色相关的同步接口：克隆音色、按描述写歌词。未配置 Mureka 时用本地模拟实现。
type VocalService interface {
	CloneVocal(ctx context.Context, description, fileName string, data []byte) (string, error)
	GenerateLyrics(ctx context.Context, prompt string) (title, lyrics string, err error)
	UploadURL(ctx context.Context, purpose, url string) (string, error)
	UploadFile(ctx context.Context, purpose, fileName string, data []byte) (string, error)
	UploadPart(ctx context.Context, uploadID, fileName string, data []byte) (json.RawMessage, error)
	CallTool(ctx context.Context, operation, taskID string, params map[string]interface{}) (json.RawMessage, error)
}

// MurekaConfig Mureka 接入配置。
type MurekaConfig struct {
	Base    string // 国内站 https://api.mureka.cn，海外站 https://api.mureka.ai
	APIKey  string
	Model   string // auto 或具体版本；mureka-o2 不支持 vocal_id
	Timeout time.Duration
}

// Mureka 实现 Provider（音色演唱创作任务）与 VocalService。
type Mureka struct {
	cfg    MurekaConfig
	client *http.Client
}

// NewMureka 创建 Mureka 上游。
func NewMureka(cfg MurekaConfig) *Mureka {
	cfg.Base = strings.TrimRight(cfg.Base, "/")
	if cfg.Model == "" {
		cfg.Model = "auto"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &Mureka{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

// Name 实现 Provider。
func (m *Mureka) Name() string { return "mureka" }

// murekaSongTask 对应文档中的 SongTask。
type murekaSongTask struct {
	ID           string       `json:"id"`
	Model        string       `json:"model"`
	Status       string       `json:"status"`
	FailedReason string       `json:"failed_reason"`
	Watermarked  bool         `json:"watermarked"`
	CreatedAt    int64        `json:"created_at"`
	Choices      []murekaSong `json:"choices"`
}

type murekaSong struct {
	Index          int             `json:"index"`
	ID             string          `json:"id"`
	URL            string          `json:"url"`
	FlacURL        string          `json:"flac_url"`
	WavURL         string          `json:"wav_url"`
	Duration       int64           `json:"duration"` // 毫秒
	LyricsSections json.RawMessage `json:"lyrics_sections"`
}

// Submit 提交音色演唱创作。一次请求产出 n 首，上游只返回一个任务号，
// 这里拆成「任务号#序号」，每首对应本平台一条任务，查询时各取各的那首。
func (m *Mureka) Submit(ctx context.Context, req *SubmitRequest) (*SubmitResult, error) {
	if req.Kind != model.KindVoiceSong {
		return nil, fmt.Errorf("Mureka 不支持的任务类型: %s", req.Kind)
	}
	n := OutputCount(req.Kind)
	payload := req.Payload
	if strings.HasPrefix(stringOf(payload, "vocal_id"), "mock-") {
		return nil, fmt.Errorf("此演唱音色为本地模拟音色，请重新上传清唱创建真实音色后提交")
	}
	body := map[string]interface{}{"n": n}
	path := "/v1/song/generate"
	prefix := ""
	switch stringOf(payload, "operation") {
	case "", "generate":
		body["lyrics"] = stringOf(payload, "lyrics")
		body["model"] = m.model(payload)
		for _, key := range []string{"prompt", "vocal_id", "gender", "reference_id", "melody_id"} {
			if v := stringOf(payload, key); v != "" {
				body[key] = v
			}
		}
		if v, ok := payload["stream"].(bool); ok {
			body["stream"] = v
		}
	case "instrumental":
		path = "/v1/instrumental/generate"
		prefix = "instrumental:"
		body["model"] = m.model(payload)
		for _, key := range []string{"prompt", "instrumental_id"} {
			if v := stringOf(payload, key); v != "" {
				body[key] = v
			}
		}
		if v, ok := payload["stream"].(bool); ok {
			body["stream"] = v
		}
	case "extend":
		path = "/v1/song/extend"
		body = map[string]interface{}{
			"lyrics":      stringOf(payload, "lyrics"),
			"extend_at":   payload["extend_at"],
			"extend_type": firstNonEmpty(stringOf(payload, "extend_type"), "tail"),
			"model":       firstNonEmpty(stringOf(payload, "model"), "mureka-8"),
		}
		copyMurekaSource(body, payload)
	case "remix":
		path = "/v1/song/remix"
		body["lyrics"] = stringOf(payload, "lyrics")
		body["prompt"] = stringOf(payload, "prompt")
		copyMurekaSource(body, payload)
	case "easy":
		path = "/v1/song/easy-generate"
		body["prompt"] = stringOf(payload, "prompt")
		body["model"] = m.model(payload)
	default:
		return nil, fmt.Errorf("Mureka 不支持的创作操作: %s", stringOf(payload, "operation"))
	}

	var task murekaSongTask
	if err := m.call(ctx, http.MethodPost, path, body, &task); err != nil {
		return nil, err
	}
	if task.ID == "" {
		return nil, fmt.Errorf("Mureka 未返回任务 ID")
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = prefix + task.ID + "#" + strconv.Itoa(i)
	}
	return &SubmitResult{ProviderIDs: ids}, nil
}

func copyMurekaSource(body, payload map[string]interface{}) {
	if v := stringOf(payload, "song_id"); v != "" {
		body["song_id"] = v
	} else if v := stringOf(payload, "upload_audio_id"); v != "" {
		body["upload_audio_id"] = v
	}
}

func (m *Mureka) model(payload map[string]interface{}) string {
	modelName := firstNonEmpty(stringOf(payload, "model"), m.cfg.Model)
	if modelName == "mureka-o2" && (stringOf(payload, "vocal_id") != "" || stringOf(payload, "melody_id") != "") {
		return "auto"
	}
	return modelName
}

// Fetch 查询任务，并按结果序号取回对应的歌曲或纯音乐。
func (m *Mureka) Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error) {
	id := req.ProviderID
	path := "/v1/song/query/"
	if strings.HasPrefix(id, "instrumental:") {
		id = strings.TrimPrefix(id, "instrumental:")
		path = "/v1/instrumental/query/"
	}
	taskID, index := splitMurekaID(id)
	var task murekaSongTask
	if err := m.call(ctx, http.MethodGet, path+taskID, nil, &task); err != nil {
		return nil, err
	}
	return murekaResult(&task, index), nil
}

// murekaResult 把上游任务状态换算成平台状态；成功时取第 index 首。
func murekaResult(task *murekaSongTask, index int) *FetchResult {
	switch task.Status {
	case "succeeded":
	case "failed", "timeouted", "cancelled":
		reason := firstNonEmpty(task.FailedReason, "Mureka 生成失败（"+task.Status+"）")
		return &FetchResult{Status: model.StatusFailed, Reason: reason}
	default:
		// preparing / queued 排队，running / streaming / reviewing 生成中
		p := &model.TaskProgress{Stage: "queued"}
		if task.Status == "running" || task.Status == "streaming" || task.Status == "reviewing" {
			p.Stage = "running"
		}
		return &FetchResult{Status: model.StatusProcessing, Progress: p}
	}

	var song *murekaSong
	for i := range task.Choices {
		if task.Choices[i].Index == index {
			song = &task.Choices[i]
			break
		}
	}
	if song == nil && index < len(task.Choices) {
		song = &task.Choices[index]
	}
	if song == nil || song.URL == "" {
		return &FetchResult{Status: model.StatusFailed, Reason: "Mureka 生成完成但没有返回这首歌"}
	}

	extend, _ := json.Marshal(map[string]interface{}{
		"provider":        "mureka",
		"mureka_task_id":  task.ID,
		"model":           task.Model,
		"watermarked":     task.Watermarked,
		"id":              song.ID,
		"audio_url":       song.URL,
		"wav_url":         song.WavURL,
		"flac_url":        song.FlacURL,
		"duration":        float64(song.Duration) / 1000,
		"lyrics_sections": song.LyricsSections,
	})
	return &FetchResult{
		Status:   model.StatusCompleted,
		CustomID: firstNonEmpty(song.ID, fmt.Sprintf("%s-%d", task.ID, index)),
		ProxyURL: song.URL,
		FileInfo: &model.FileInfo{MP3URL: song.URL, WAVURL: song.WavURL, Duration: float64(song.Duration) / 1000},
		Extend:   string(extend),
	}
}

func splitMurekaID(id string) (string, int) {
	if i := strings.LastIndexByte(id, '#'); i > 0 {
		if n, err := strconv.Atoi(id[i+1:]); err == nil {
			return id[:i], n
		}
	}
	return id, 0
}

// CloneVocal 上传一段清唱，创建可复用的 vocal_id。文件需为 mp3/m4a，小于 10MB，时长 15~30 秒（超出部分会被裁掉）。
func (m *Mureka) CloneVocal(ctx context.Context, description, fileName string, data []byte) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if description != "" {
		_ = w.WriteField("description", description)
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.Base+"/v1/song/vocal-clone", &buf)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", w.FormDataContentType())
	var resp struct {
		VocalID string `json:"vocal_id"`
	}
	if err := m.send(httpReq, &resp); err != nil {
		return "", err
	}
	if resp.VocalID == "" {
		return "", fmt.Errorf("Mureka 未返回 vocal_id")
	}
	return resp.VocalID, nil
}

// GenerateLyrics 按一句描述写歌词（灵感模式用）。
func (m *Mureka) GenerateLyrics(ctx context.Context, prompt string) (string, string, error) {
	var resp struct {
		Title  string `json:"title"`
		Lyrics string `json:"lyrics"`
	}
	if err := m.call(ctx, http.MethodPost, "/v1/lyrics/generate", map[string]interface{}{"prompt": prompt}, &resp); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(resp.Lyrics) == "" {
		return "", "", fmt.Errorf("Mureka 未返回歌词")
	}
	return resp.Title, resp.Lyrics, nil
}

type murekaToolEndpoint struct {
	method string
	path   string
	taskID bool
}

// 仅列出官方文档中的固定接口；客户端不能传自定义路径或上游密钥。
var murekaTools = map[string]murekaToolEndpoint{
	"lyrics_generate":       {http.MethodPost, "/v1/lyrics/generate", false},
	"lyrics_extend":         {http.MethodPost, "/v1/lyrics/extend", false},
	"song_generate":         {http.MethodPost, "/v1/song/generate", false},
	"song_easy":             {http.MethodPost, "/v1/song/easy-generate", false},
	"soundtrack":            {http.MethodPost, "/v1/soundtrack/generate", false},
	"song_query":            {http.MethodGet, "/v1/song/query/", true},
	"song_extend":           {http.MethodPost, "/v1/song/extend", false},
	"song_recognize":        {http.MethodPost, "/v1/song/recognize", false},
	"song_describe":         {http.MethodPost, "/v1/song/describe", false},
	"song_transcribe":       {http.MethodPost, "/v1/song/transcribe", false},
	"song_stem":             {http.MethodPost, "/v1/song/stem", false},
	"track_generate":        {http.MethodPost, "/v1/track/generate", false},
	"region_edit":           {http.MethodPost, "/v1/song/region-edit", false},
	"song_remix":            {http.MethodPost, "/v1/song/remix", false},
	"video_generate":        {http.MethodPost, "/v1/video/generate", false},
	"video_query":           {http.MethodGet, "/v1/video/query/", true},
	"lyrics_video":          {http.MethodPost, "/v1/lyrics-video/generate", false},
	"instrumental_generate": {http.MethodPost, "/v1/instrumental/generate", false},
	"instrumental_query":    {http.MethodGet, "/v1/instrumental/query/", true},
	"uploads_create":        {http.MethodPost, "/v1/uploads/create", false},
	"uploads_complete":      {http.MethodPost, "/v1/uploads/complete", false},
	"finetuning_create":     {http.MethodPost, "/v1/finetuning/create", false},
	"finetuning_query":      {http.MethodGet, "/v1/finetuning/query/", true},
	"tts_generate":          {http.MethodPost, "/v1/tts/generate", false},
	"tts_podcast":           {http.MethodPost, "/v1/tts/podcast", false},
	"account_billing":       {http.MethodGet, "/v1/account/billing", false},
}

var murekaTaskIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

// CallTool 为后台高级工具提供固定的 Mureka 能力，原始结果供运营查看。
func (m *Mureka) CallTool(ctx context.Context, operation, taskID string, params map[string]interface{}) (json.RawMessage, error) {
	endpoint, ok := murekaTools[operation]
	if !ok {
		return nil, fmt.Errorf("不支持的高级工具: %s", operation)
	}
	path := endpoint.path
	if endpoint.taskID {
		if !murekaTaskIDPattern.MatchString(taskID) {
			return nil, fmt.Errorf("任务 ID 格式不正确")
		}
		path += taskID
	}
	var body interface{}
	if endpoint.method == http.MethodPost {
		if params == nil {
			params = map[string]interface{}{}
		}
		body = params
	}
	var out json.RawMessage
	if err := m.call(ctx, endpoint.method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UploadURL 把项目已有的音频或图片 URL 上传给 Mureka，供参考音乐、续写和混音使用。
func (m *Mureka) UploadURL(ctx context.Context, purpose, url string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("purpose", purpose)
	_ = w.WriteField("url", url)
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.Base+"/v1/files/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var resp struct {
		ID string `json:"id"`
	}
	if err := m.send(req, &resp); err != nil {
		return "", err
	}
	if resp.ID == "" {
		return "", fmt.Errorf("Mureka 未返回文件 ID")
	}
	return resp.ID, nil
}

// UploadFile 上传本地文件。大于 10MB 的素材应使用 URL 或分块上传。
func (m *Mureka) UploadFile(ctx context.Context, purpose, fileName string, data []byte) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("purpose", purpose)
	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.Base+"/v1/files/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var resp struct {
		ID string `json:"id"`
	}
	if err := m.send(req, &resp); err != nil {
		return "", err
	}
	if resp.ID == "" {
		return "", fmt.Errorf("Mureka 未返回文件 ID")
	}
	return resp.ID, nil
}

// UploadPart 给已经创建的上传对象追加一个文件块。
func (m *Mureka) UploadPart(ctx context.Context, uploadID, fileName string, data []byte) (json.RawMessage, error) {
	if !murekaTaskIDPattern.MatchString(uploadID) {
		return nil, fmt.Errorf("上传 ID 格式不正确")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("upload_id", uploadID)
	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.Base+"/v1/uploads/add", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var resp json.RawMessage
	if err := m.send(req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (m *Mureka) call(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, m.cfg.Base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	return m.send(httpReq, out)
}

func (m *Mureka) send(httpReq *http.Request, out interface{}) error {
	httpReq.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)
	httpReq.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求 Mureka 失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Mureka 返回 HTTP %d：%s", resp.StatusCode, murekaError(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 Mureka 响应失败: %s", truncate(string(data), 300))
	}
	return nil
}

// murekaError 尽量取出错误信息；文档未给出错误结构，兼容常见的几种写法。
func murekaError(data []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(data, &e) == nil {
		if msg := firstNonEmpty(e.Error.Message, e.Message, e.Detail); msg != "" {
			return msg
		}
	}
	return truncate(string(data), 300)
}

// MockVocal 未配置 Mureka 时的本地模拟：克隆直接返回假 vocal_id，歌词按描述套模板。
type MockVocal struct{}

// CloneVocal 实现 VocalService。
func (MockVocal) CloneVocal(context.Context, string, string, []byte) (string, error) {
	return "mock-vocal-" + randomHex(6), nil
}

// GenerateLyrics 实现 VocalService。
func (MockVocal) GenerateLyrics(_ context.Context, prompt string) (string, string, error) {
	return "", "[Verse]\n" + prompt + "\n\n[Chorus]\n" + prompt, nil
}

// UploadURL 实现本地模拟上传。
func (MockVocal) UploadURL(_ context.Context, purpose, url string) (string, error) {
	return "mock-" + purpose + "-" + randomHex(6), nil
}

// CallTool 实现本地模拟工具响应。
func (MockVocal) CallTool(_ context.Context, operation, taskID string, params map[string]interface{}) (json.RawMessage, error) {
	if _, ok := murekaTools[operation]; !ok {
		return nil, fmt.Errorf("不支持的高级工具: %s", operation)
	}
	return json.RawMessage(`{"mock":true}`), nil
}

// UploadFile 实现本地模拟文件上传。
func (MockVocal) UploadFile(_ context.Context, purpose, fileName string, data []byte) (string, error) {
	return "mock-" + purpose + "-" + randomHex(6), nil
}

// UploadPart 实现本地模拟分块上传。
func (MockVocal) UploadPart(_ context.Context, uploadID, fileName string, data []byte) (json.RawMessage, error) {
	return json.RawMessage(`{"mock":true}`), nil
}
