package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

func TestExtractTaskIDs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"数字数组", `[199824, 199825]`, []string{"199824", "199825"}},
		{"单个数字", `1344761`, []string{"1344761"}},
		{"task_ids 字段", `{"task_ids": ["205041", "205042"]}`, []string{"205041", "205042"}},
		{"task_id 字段", `{"task_id": "207031", "status": "pending"}`, []string{"207031"}},
		{"id 字段", `{"id": 199824}`, []string{"199824"}},
		{"空对象", `{}`, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var data interface{}
			if err := json.Unmarshal([]byte(tc.raw), &data); err != nil {
				t.Fatalf("解析测试数据失败: %v", err)
			}
			got := extractTaskIDs(data)
			if len(got) != len(tc.want) {
				t.Fatalf("数量不符: got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("第 %d 个不符: got %s, want %s", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeStatus(t *testing.T) {
	cases := []struct {
		raw  interface{}
		want model.TaskStatus
	}{
		{"completed", model.StatusCompleted},
		{"complete", model.StatusCompleted},
		{"failed", model.StatusFailed},
		{"processing", model.StatusProcessing},
		{"queued", model.StatusPending},
		{float64(3), model.StatusCompleted},
		{float64(4), model.StatusFailed},
		{float64(2), model.StatusProcessing},
		{float64(1), model.StatusPending},
	}

	for _, tc := range cases {
		if got := normalizeStatus(tc.raw); got != tc.want {
			t.Errorf("normalizeStatus(%v) = %s, want %s", tc.raw, got, tc.want)
		}
	}
}

func TestOutputCount(t *testing.T) {
	if n := OutputCount(model.KindGenerate); n != 2 {
		t.Errorf("生成音乐应产出 2 个任务，实际 %d", n)
	}
	if n := OutputCount(model.KindUpsample); n != 2 {
		t.Errorf("Remaster 应产出 2 个任务，实际 %d", n)
	}
	if n := OutputCount(model.KindCrop); n != 1 {
		t.Errorf("裁剪应产出 1 个任务，实际 %d", n)
	}
}

func TestMockLifecycle(t *testing.T) {
	m := NewMock("https://cdn.test/suno", 50*time.Millisecond)
	ctx := context.Background()

	res, err := m.Submit(ctx, &SubmitRequest{
		Kind:    model.KindGenerate,
		Payload: map[string]interface{}{"title": "夏日回忆", "mv": "chirp-hawk"},
	})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if len(res.ProviderIDs) != 2 {
		t.Fatalf("生成音乐应返回 2 个上游 ID，实际 %d", len(res.ProviderIDs))
	}

	// 立刻查询应当还在处理中
	first, err := m.Fetch(ctx, &FetchRequest{Kind: model.KindGenerate, ProviderID: res.ProviderIDs[0]})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if first.Status != model.StatusProcessing {
		t.Fatalf("期望 processing，实际 %s", first.Status)
	}

	time.Sleep(80 * time.Millisecond)

	done, err := m.Fetch(ctx, &FetchRequest{Kind: model.KindGenerate, ProviderID: res.ProviderIDs[0]})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if done.Status != model.StatusCompleted {
		t.Fatalf("期望 completed，实际 %s", done.Status)
	}
	if done.CustomID == "" {
		t.Error("完成后应返回 custom_id")
	}
	if done.FileInfo == nil || done.FileInfo.MP3URL == "" {
		t.Error("完成后应返回 mp3 地址")
	}
	if !json.Valid([]byte(done.Extend)) {
		t.Errorf("extend 应是合法 JSON 字符串，实际: %s", done.Extend)
	}
}

func TestMockVideoResult(t *testing.T) {
	m := NewMock("https://cdn.test/suno", time.Millisecond)
	ctx := context.Background()

	res, err := m.Submit(ctx, &SubmitRequest{Kind: model.KindVideo, Payload: map[string]interface{}{"suno_id": "abc"}})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	got, err := m.Fetch(ctx, &FetchRequest{Kind: model.KindVideo, ProviderID: res.ProviderIDs[0]})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.CustomID != "" {
		t.Error("视频任务不应产生新的 custom_id")
	}

	var extend struct {
		Status   string `json:"status"`
		VideoURL string `json:"video_url"`
	}
	if err := json.Unmarshal([]byte(got.Extend), &extend); err != nil {
		t.Fatalf("解析 extend 失败: %v", err)
	}
	if extend.Status != "complete" || extend.VideoURL == "" {
		t.Errorf("extend 内容不符: %+v", extend)
	}
}

func TestMockUnknownTask(t *testing.T) {
	m := NewMock("https://cdn.test/suno", time.Millisecond)
	got, err := m.Fetch(context.Background(), &FetchRequest{Kind: model.KindGenerate, ProviderID: "not-exist"})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Status != model.StatusFailed {
		t.Errorf("未知任务应返回 failed，实际 %s", got.Status)
	}
}
