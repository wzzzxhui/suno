package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

// TMEConfig 腾讯多媒体实验室「唱歌克隆」的接入参数，均来自 /register 注册结果。
type TMEConfig struct {
	Base             string
	GatewaySecretID  string
	GatewaySecretKey string
	Source           string
	SecretID         string
	SecretKey        string
	ContentID        string
	// OutputBase 是 COS 仓库根目录对应的可访问地址，用来把结果文件名拼成下载地址
	OutputBase string
	OutputDir  string
	Timeout    time.Duration
	// Signer 不为空时，结果地址会签成带时效的私有读链接
	Signer *COSSigner
}

// TME 对接唱歌克隆：训练音色模型（type=2）与用模型转换歌曲（type=1）。
// 文档：https://multimedia.tencent.com/zh/docs/smart-music/api/11-singing-cloning/
type TME struct {
	cfg    TMEConfig
	client *http.Client
}

// NewTME 创建唱歌克隆客户端。
func NewTME(cfg TMEConfig) *TME {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	cfg.Base = strings.TrimRight(cfg.Base, "/")
	cfg.OutputBase = strings.TrimRight(cfg.OutputBase, "/")
	return &TME{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

// Name 实现 Provider。
func (t *TME) Name() string { return "tme" }

// 唱歌克隆任务类型与开关取值
const (
	tmeTypeGenerate = 1
	tmeTypeTrain    = 2
	tmeSwitchOff    = 1
	tmeSwitchOn     = 2
)

// 任务状态：1=SUBMITTED 2=PROCESSING 3=COMPLETED 4=ERROR 5=CANCELED
const (
	tmeStateCompleted = 3
	tmeStateError     = 4
	tmeStateCanceled  = 5
)

type tmeJob struct {
	ID       string      `json:"id"`
	State    int         `json:"state"`
	CustomID string      `json:"customId"`
	Outputs  []tmeOutput `json:"outputs"`
	// 文档未给出错误结构，失败时尽量把可能的字段透出来
	Message string `json:"message"`
	Error   string `json:"error"`
}

type tmeOutput struct {
	Destination        string `json:"destination"`
	SmartContentResult struct {
		SingingCloning struct {
			SongName  string `json:"songName"`
			ModelName string `json:"modelName"`
		} `json:"singingCloning"`
	} `json:"smartContentResult"`
}

// Submit 创建唱歌克隆任务。
func (t *TME) Submit(ctx context.Context, req *SubmitRequest) (*SubmitResult, error) {
	audioURL := stringOf(req.Payload, "audio_url")
	modelName := stringOf(req.Payload, "model_name")
	if audioURL == "" || modelName == "" {
		return nil, fmt.Errorf("缺少 audio_url 或 model_name")
	}

	customID := "sc_" + randomHex(12)
	cloning := map[string]interface{}{"modelName": modelName}
	output := map[string]interface{}{"inputSelectors": []int{0}}

	switch req.Kind {
	case model.KindVoiceTrain:
		cloning["type"] = tmeTypeTrain
		if epoch := int(toFloat(req.Payload["total_epoch"])); epoch > 0 {
			cloning["totalEpoch"] = epoch
		}
	case model.KindVoiceCover:
		cloning["type"] = tmeTypeGenerate
		cloning["useSeparate"] = tmeSwitchOff
		if req.Payload["separate"] == true {
			cloning["useSeparate"] = tmeSwitchOn
		}
		// 每个任务单独一个目录，结果文件名重复也不会互相覆盖
		output["contentId"] = t.cfg.ContentID
		output["destination"] = t.cfg.OutputDir + "/" + customID
		output["smartContentDescriptor"] = map[string]interface{}{"outputPrefix": "cover"}
	default:
		return nil, fmt.Errorf("唱歌克隆不支持任务类型: %s", req.Kind)
	}

	descriptor, _ := output["smartContentDescriptor"].(map[string]interface{})
	if descriptor == nil {
		descriptor = map[string]interface{}{}
	}
	descriptor["singingCloning"] = cloning
	output["smartContentDescriptor"] = descriptor

	var resp struct {
		CreateJobResponse struct {
			Job tmeJob `json:"job"`
		} `json:"createJobResponse"`
	}
	body := map[string]interface{}{
		"action": "CreateJob",
		"createJobRequest": map[string]interface{}{
			"customId": customID,
			"inputs":   []map[string]interface{}{{"url": audioURL}},
			"outputs":  []map[string]interface{}{output},
		},
	}
	if err := t.call(ctx, "/job", body, &resp); err != nil {
		return nil, err
	}
	if resp.CreateJobResponse.Job.ID == "" {
		return nil, fmt.Errorf("唱歌克隆未返回任务 ID")
	}
	return &SubmitResult{ProviderIDs: []string{resp.CreateJobResponse.Job.ID}}, nil
}

// Fetch 按任务 ID 查询唱歌克隆任务。
func (t *TME) Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error) {
	var resp struct {
		GetJobResponse struct {
			Job json.RawMessage `json:"job"`
		} `json:"getJobResponse"`
	}
	body := map[string]interface{}{
		"action":        "GetJob",
		"getJobRequest": map[string]interface{}{"id": req.ProviderID},
	}
	if err := t.call(ctx, "/job", body, &resp); err != nil {
		return nil, err
	}

	var job tmeJob
	if err := json.Unmarshal(resp.GetJobResponse.Job, &job); err != nil || job.ID == "" {
		return nil, fmt.Errorf("解析唱歌克隆任务失败")
	}

	result := &FetchResult{Status: model.StatusProcessing, Extend: string(resp.GetJobResponse.Job)}
	switch job.State {
	case tmeStateCompleted:
	case tmeStateError, tmeStateCanceled:
		result.Status = model.StatusFailed
		result.Reason = firstNonEmpty(job.Message, job.Error, "唱歌克隆任务失败")
		if job.State == tmeStateCanceled {
			result.Reason = "唱歌克隆任务已取消"
		}
		return result, nil
	default:
		return result, nil
	}

	result.Status = model.StatusCompleted
	if req.Kind == model.KindVoiceCover {
		if len(job.Outputs) == 0 || job.Outputs[0].SmartContentResult.SingingCloning.SongName == "" {
			result.Status = model.StatusFailed
			result.Reason = "唱歌克隆完成但未返回结果文件"
			return result, nil
		}
		out := job.Outputs[0]
		url := t.outputURL(out.Destination, out.SmartContentResult.SingingCloning.SongName)
		result.ProxyURL = url
		result.FileInfo = &model.FileInfo{MP3URL: url}
	}
	return result, nil
}

// outputURL 把仓库内的目录与文件名拼成可访问地址。
func (t *TME) outputURL(destination, songName string) string {
	path := strings.Trim(destination, "/")
	if path != "" {
		path += "/"
	}
	path += strings.TrimLeft(songName, "/")
	if t.cfg.OutputBase == "" {
		return path
	}
	full := t.cfg.OutputBase + "/" + path
	if t.cfg.Signer.Enabled() {
		if signed, err := t.cfg.Signer.Sign(full, time.Now()); err == nil {
			return signed
		}
	}
	return full
}

// call 发送一次带签名的请求。临时密钥按文档放在请求体里。
func (t *TME) call(ctx context.Context, path string, body map[string]interface{}, out interface{}) error {
	body["secretId"] = t.cfg.SecretID
	body["secretKey"] = t.cfg.SecretKey
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Date", date)
	httpReq.Header.Set("Source", t.cfg.Source)
	httpReq.Header.Set("Authorization", t.sign(date))

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求唱歌克隆失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("唱歌克隆返回 HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析唱歌克隆响应失败: %s", truncate(string(data), 300))
	}
	return nil
}

// sign 按 API 网关 HMAC-SHA1 规则生成 Authorization 头。
func (t *TME) sign(date string) string {
	mac := hmac.New(sha1.New, []byte(t.cfg.GatewaySecretKey))
	mac.Write([]byte("date: " + date + "\nsource: " + t.cfg.Source))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf(`hmac id="%s", algorithm="hmac-sha1", headers="date source", signature="%s"`,
		t.cfg.GatewaySecretID, signature)
}

func stringOf(payload map[string]interface{}, key string) string {
	v, _ := payload[key].(string)
	return strings.TrimSpace(v)
}

// ListModels 列出账号下已训练的唱歌模型，用于自检凭证与开通状态。
func (t *TME) ListModels(ctx context.Context) ([]string, error) {
	var resp struct {
		ListModelsResponse struct {
			Total  int `json:"total"`
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		} `json:"listModelsResponse"`
	}
	body := map[string]interface{}{
		"action":            "ListModels",
		"listModelsRequest": map[string]interface{}{"offset": 0, "limit": 100, "type": 2},
	}
	if err := t.call(ctx, "/music_model", body, &resp); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.ListModelsResponse.Models))
	for _, m := range resp.ListModelsResponse.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
