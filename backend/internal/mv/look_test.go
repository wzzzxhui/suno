package mv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lepro/suno-open-api/internal/model"
)

func TestNormalizeLook(t *testing.T) {
	l, err := NormalizeLook(7, model.MVLook{Character: "  短发女生 ", RefKeys: []string{"mv/refs/m7/a.png", "mv/refs/m7/a.png", ""}})
	if err != nil {
		t.Fatal(err)
	}
	if l.Style != model.MVStyleRealistic || l.Character != "短发女生" || len(l.RefKeys) != 1 {
		t.Fatalf("清理结果不对: %+v", l)
	}

	bad := []model.MVLook{
		{Style: "oil"},
		{RefKeys: []string{"mv/refs/m8/a.png"}},       // 其他商户的图
		{RefKeys: []string{"mv/refs/m7/../m8/a.png"}}, // 路径穿越
		{RefKeys: []string{"mv/refs/m7/1.png", "mv/refs/m7/2.png", "mv/refs/m7/3.png", "mv/refs/m7/4.png"}},
		{Character: strings.Repeat("长", maxLookField+1)},
	}
	for i, b := range bad {
		if _, err := NormalizeLook(7, b); err == nil {
			t.Errorf("第 %d 个非法设定应报错: %+v", i, b)
		}
	}
}

func TestComposeImagePrompt(t *testing.T) {
	look := model.MVLook{Style: model.MVStyleRealistic, RefKeys: []string{"k"}}
	p := ComposeImagePrompt("画风：真实摄影；主角：短发女生", "主角在雨中奔跑。中景。", look)
	for _, want := range []string{"主角：短发女生", "镜头：主角在雨中奔跑", "参考图是同一个人", "真实的皮肤纹理", "塑料皮肤", "文字"} {
		if !strings.Contains(p, want) {
			t.Errorf("提示词缺少 %q:\n%s", want, p)
		}
	}

	// 早期草稿的镜头提示词已含全片设定，不重复拼接
	old := ComposeImagePrompt("统一设定", "统一设定。画面表现歌词意境", model.MVLook{})
	if strings.Count(old, "统一设定") != 1 || strings.Contains(old, "参考图") {
		t.Errorf("旧提示词处理不对:\n%s", old)
	}
}

func TestSubmitPayload(t *testing.T) {
	p := &model.MVProject{Ratio: "16:9", Resolution: "480p", VisualBible: "设定", UseCover: true, CoverURL: "cover",
		Look: model.MVLook{Style: model.MVStyleRealistic, RefKeys: []string{"k1", "k2"}}}
	refs := []string{"r1", "r2"}

	img := SubmitPayload(p, &model.MVSegment{Seq: 0, Kind: model.ShotImage, Prompt: "镜头"}, refs, "sd")
	if got := img["reference_images"].([]string); len(got) != 2 || got[0] != "r1" {
		t.Fatalf("图片镜头应带全部参考图而不是封面: %v", img)
	}
	if img["image_size"] != "1K" || !strings.Contains(img["prompt"].(string), "设定") {
		t.Fatalf("图片参数不对: %v", img)
	}

	// 有参考图的视频镜头：先画首帧，再以首帧生成视频
	seg := &model.MVSegment{Seq: 1, Kind: model.ShotVideo, Prompt: "主角转身", Start: 0, End: 10}
	kf := SubmitPayload(p, seg, refs, "sd")
	if _, ok := kf["model"]; ok || !strings.Contains(kf["prompt"].(string), "第一帧") {
		t.Fatalf("首帧阶段应提交绘画参数: %v", kf)
	}
	seg.KeyframeURL = "frame"
	vid := SubmitPayload(p, seg, refs, "sd")
	if vid["model"] != "sd" || vid["duration"] != 10 {
		t.Fatalf("视频参数不对: %v", vid)
	}
	if got := vid["reference_images"].([]string); len(got) != 1 || got[0] != "frame" {
		t.Fatalf("视频应以首帧为参考: %v", vid)
	}
	if strings.Contains(vid["prompt"].(string), "设定") {
		t.Fatalf("以首帧生成的视频不必重复全片设定: %v", vid["prompt"])
	}

	// 没有参考图时保持原来的做法：纯文生视频，首镜参考封面
	p.Look.RefKeys = nil
	first := SubmitPayload(p, &model.MVSegment{Seq: 0, Kind: model.ShotVideo, Prompt: "开场", End: 8}, nil, "sd")
	if first["model"] != "sd" || first["reference_images"].([]string)[0] != "cover" {
		t.Fatalf("首镜应参考封面: %v", first)
	}
	if _, ok := SubmitPayload(p, &model.MVSegment{Seq: 2, Kind: model.ShotVideo, End: 8}, nil, "sd")["reference_images"]; ok {
		t.Fatal("非首镜不应带参考图")
	}
}

func TestLLMWriter(t *testing.T) {
	segs := Plan(30, lyrics, PlanOptions{Tier: model.MVTierStandard})
	var gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotUser = req.Messages[1].Content

		shots := []map[string]interface{}{}
		for _, s := range segs[:len(segs)-1] { // 故意漏写最后一个镜头
			shots = append(shots, map[string]interface{}{"seq": s.Seq, "prompt": "主角在海边奔跑"})
		}
		content, _ := json.Marshal(map[string]interface{}{"visual_bible": "画风：真实摄影；主角：短发女生", "shots": shots})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]string{"content": "```json\n" + string(content) + "\n```"}}},
		})
	}))
	defer srv.Close()

	w := &LLMWriter{BaseURL: srv.URL + "/v1", APIKey: "key", Model: "m"}
	look := model.MVLook{Style: model.MVStyleAnime, Outfit: "校服", RefKeys: []string{"k"}}
	sb, err := w.Write(context.Background(), SongInfo{Title: "夏日", Tags: "pop", Look: look}, segs)
	if err != nil {
		t.Fatal(err)
	}
	if sb.Writer != "llm" || sb.VisualBible != "画风：真实摄影；主角：短发女生" {
		t.Fatalf("分镜结果不对: %+v", sb)
	}
	if sb.Prompts[segs[0].Seq] != "主角在海边奔跑" || sb.Prompts[segs[len(segs)-1].Seq] == "" {
		t.Fatalf("漏写的镜头应由模板补上: %v", sb.Prompts)
	}
	for _, want := range []string{"日系动画", "校服", "参考照片", "#0 [视频]", "歌词：第一句"} {
		if !strings.Contains(gotUser, want) {
			t.Errorf("发给大模型的内容缺少 %q:\n%s", want, gotUser)
		}
	}

	// 接口出错时返回错误，由 Service 退回模板
	bad := &LLMWriter{BaseURL: srv.URL + "/v1", APIKey: "wrong", Model: "m"}
	if _, err := bad.Write(context.Background(), SongInfo{}, segs); err == nil {
		t.Fatal("鉴权失败应返回错误")
	}
}
