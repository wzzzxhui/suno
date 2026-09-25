package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

// TestFetchParsesRealUpstreamShape 用真实上游响应样本回归解析逻辑。
// 上游把任务状态放在 data 上，歌曲产出放在 data.result 里。
func TestFetchParsesRealUpstreamShape(t *testing.T) {
	fixture, err := os.ReadFile("testdata_task.json")
	if err != nil {
		t.Skipf("缺少样本文件，跳过：%v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("id"); got != "139434909" {
			t.Errorf("查询参数 id 应透传，实际 %q", got)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("鉴权头不符：%q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := NewSuno(server.URL, "test-key", 5*time.Second)
	got, err := client.Fetch(context.Background(), &FetchRequest{
		Kind:       model.KindGenerate,
		ProviderID: "139434909",
	})
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}

	if got.Status != model.StatusCompleted {
		t.Errorf("状态应为 completed，实际 %s", got.Status)
	}
	if got.CustomID != "4852c1a7-cc06-4540-9e88-377ee9ebdefa" {
		t.Errorf("custom_id 解析失败，实际 %q", got.CustomID)
	}
	if got.ProxyURL == "" {
		t.Error("proxy_url 应被解析出来")
	}
	if got.FileInfo == nil {
		t.Fatal("fileInfo 应被解析出来")
	}
	if got.FileInfo.MP3URL == "" {
		t.Error("音频地址应被解析出来")
	}
	// 封面在上游落在 cosUrl 上，需要映射到 CoverURL
	if got.FileInfo.CoverURL == "" {
		t.Error("封面地址应从 cosUrl / bigUrl 映射出来")
	}
	if got.FileInfo.Duration <= 0 {
		t.Errorf("时长应被解析出来，实际 %v", got.FileInfo.Duration)
	}
	if got.Extend == "" || !json.Valid([]byte(got.Extend)) {
		t.Errorf("extend 应是合法 JSON 字符串，实际 %q", truncate(got.Extend, 80))
	}
}

// TestFetchHandlesFlatShape 兜住产出直接平铺在 data 上的情况。
func TestFetchHandlesFlatShape(t *testing.T) {
	body := `{"code":200,"message":"success","data":{
		"status":"completed",
		"custom_id":"flat-uuid",
		"proxy_url":"https://example.com/p.mp3",
		"extend":"[{\"status\":\"complete\"}]",
		"fileInfo":{"mp3Url":"https://example.com/a.mp3","coverUrl":"https://example.com/c.png","duration":100.5}
	}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := NewSuno(server.URL, "k", 5*time.Second)
	got, err := client.Fetch(context.Background(), &FetchRequest{Kind: model.KindGenerate, ProviderID: "1"})
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if got.CustomID != "flat-uuid" {
		t.Errorf("平铺结构下 custom_id 解析失败，实际 %q", got.CustomID)
	}
	if got.FileInfo == nil || got.FileInfo.MP3URL == "" {
		t.Error("平铺结构下 fileInfo 解析失败")
	}
}

// TestFetchFailedTaskCarriesReason 失败任务应带出原因。
func TestFetchFailedTaskCarriesReason(t *testing.T) {
	body := `{"code":200,"message":"success","data":{
		"status":"failed",
		"error":"上游生成超时",
		"result":{"status":4,"errormsg":"timeout"}
	}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := NewSuno(server.URL, "k", 5*time.Second)
	got, err := client.Fetch(context.Background(), &FetchRequest{Kind: model.KindGenerate, ProviderID: "1"})
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if got.Status != model.StatusFailed {
		t.Errorf("状态应为 failed，实际 %s", got.Status)
	}
	if got.Reason == "" {
		t.Error("失败原因不应为空")
	}
}

func TestSeedancePayload(t *testing.T) {
	got := seedancePayload(map[string]interface{}{
		"prompt": "海边黄昏", "ratio": "16:9", "resolution": "480p", "duration": 12,
		"reference_images": []interface{}{"https://x/a.png"}, "created_by": "admin:x",
	})
	content := got["content"].([]map[string]interface{})
	if content[0]["text"] != "海边黄昏 --ratio 16:9 --rs 480p --dur 12" {
		t.Fatalf("提示词参数拼接错误: %v", content[0]["text"])
	}
	if len(content) != 2 || content[1]["type"] != "image_url" {
		t.Fatalf("参考图应作为 image_url 元素: %v", content)
	}
	if got["model"] != "doubao-seedance-1-0-pro-fast-251015" || got["created_by"] != nil {
		t.Fatalf("模型或多余字段不对: %v", got)
	}

	// 表单提交的时长是 float64
	if c := seedancePayload(map[string]interface{}{"prompt": "猫", "duration": float64(5)})["content"].([]map[string]interface{}); c[0]["text"] != "猫 --dur 5" {
		t.Fatalf("float 时长拼接错误: %v", c[0]["text"])
	}
	// 已是上游格式时原样透传
	raw := map[string]interface{}{"content": []interface{}{}, "model": "m"}
	if out := seedancePayload(raw); out["model"] != "m" {
		t.Fatal("上游格式应原样透传")
	}
}
