package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lepro/suno-open-api/internal/model"
)

func newTMEServer(t *testing.T, handle func(body map[string]interface{}) interface{}) (*TME, *[]map[string]interface{}) {
	t.Helper()
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验网关签名
		date, source := r.Header.Get("Date"), r.Header.Get("Source")
		mac := hmac.New(sha1.New, []byte("gw-key"))
		mac.Write([]byte("date: " + date + "\nsource: " + source))
		want := fmt.Sprintf(`hmac id="gw-id", algorithm="hmac-sha1", headers="date source", signature="%s"`,
			base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		if r.Header.Get("Authorization") != want || source != "uin_demo" || !strings.HasSuffix(date, " GMT") {
			http.Error(w, `{"message":"bad sign"}`, http.StatusUnauthorized)
			return
		}

		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_ = json.NewEncoder(w).Encode(handle(body))
	}))
	t.Cleanup(srv.Close)

	return NewTME(TMEConfig{
		Base: srv.URL, GatewaySecretID: "gw-id", GatewaySecretKey: "gw-key", Source: "uin_demo",
		SecretID: "tmp-id", SecretKey: "tmp-key", ContentID: "content-1",
		OutputBase: "https://bucket.cos.ap-guangzhou.myqcloud.com/", OutputDir: "/voice_cover",
	}), &bodies
}

func TestTMESubmitCover(t *testing.T) {
	tme, bodies := newTMEServer(t, func(map[string]interface{}) interface{} {
		return map[string]interface{}{"createJobResponse": map[string]interface{}{"job": map[string]interface{}{"id": "job-1", "state": 1}}}
	})

	res, err := tme.Submit(context.Background(), &SubmitRequest{Kind: model.KindVoiceCover, Payload: map[string]interface{}{
		"audio_url": "https://cdn.example.com/a.mp3", "model_name": "m1_abc", "separate": true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ProviderIDs) != 1 || res.ProviderIDs[0] != "job-1" {
		t.Fatalf("任务 ID 解析错误: %+v", res.ProviderIDs)
	}

	body := (*bodies)[0]
	if body["action"] != "CreateJob" || body["secretId"] != "tmp-id" || body["secretKey"] != "tmp-key" {
		t.Fatalf("公共参数错误: %v", body)
	}
	out := body["createJobRequest"].(map[string]interface{})["outputs"].([]interface{})[0].(map[string]interface{})
	cloning := out["smartContentDescriptor"].(map[string]interface{})["singingCloning"].(map[string]interface{})
	if cloning["type"].(float64) != 1 || cloning["modelName"] != "m1_abc" || cloning["useSeparate"].(float64) != 2 {
		t.Fatalf("唱歌克隆参数错误: %v", cloning)
	}
	if out["contentId"] != "content-1" || !strings.HasPrefix(out["destination"].(string), "/voice_cover/sc_") {
		t.Fatalf("输出仓库参数错误: %v", out)
	}
}

func TestTMESubmitTrain(t *testing.T) {
	tme, bodies := newTMEServer(t, func(map[string]interface{}) interface{} {
		return map[string]interface{}{"createJobResponse": map[string]interface{}{"job": map[string]interface{}{"id": "job-2"}}}
	})

	if _, err := tme.Submit(context.Background(), &SubmitRequest{Kind: model.KindVoiceTrain, Payload: map[string]interface{}{
		"audio_url": "https://cdn.example.com/dry.wav", "model_name": "m1_new", "total_epoch": 20,
	}}); err != nil {
		t.Fatal(err)
	}
	out := (*bodies)[0]["createJobRequest"].(map[string]interface{})["outputs"].([]interface{})[0].(map[string]interface{})
	cloning := out["smartContentDescriptor"].(map[string]interface{})["singingCloning"].(map[string]interface{})
	if cloning["type"].(float64) != 2 || cloning["totalEpoch"].(float64) != 20 {
		t.Fatalf("训练参数错误: %v", cloning)
	}
	if _, ok := out["contentId"]; ok {
		t.Error("训练任务不应带输出仓库")
	}
}

func TestTMEFetch(t *testing.T) {
	state := 2
	tme, _ := newTMEServer(t, func(map[string]interface{}) interface{} {
		job := map[string]interface{}{"id": "job-1", "state": state, "outputs": []interface{}{map[string]interface{}{
			"destination":        "/voice_cover/sc_x",
			"smartContentResult": map[string]interface{}{"singingCloning": map[string]interface{}{"songName": "out.mp3"}},
		}}}
		return map[string]interface{}{"getJobResponse": map[string]interface{}{"job": job}}
	})
	fetch := func() *FetchResult {
		res, err := tme.Fetch(context.Background(), &FetchRequest{Kind: model.KindVoiceCover, ProviderID: "job-1"})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	if got := fetch(); got.Status != model.StatusProcessing {
		t.Fatalf("进行中状态映射错误: %s", got.Status)
	}

	state = 3
	got := fetch()
	want := "https://bucket.cos.ap-guangzhou.myqcloud.com/voice_cover/sc_x/out.mp3"
	if got.Status != model.StatusCompleted || got.FileInfo == nil || got.FileInfo.MP3URL != want || got.CustomID != "" {
		t.Fatalf("完成结果错误: %+v", got)
	}

	state = 4
	if got := fetch(); got.Status != model.StatusFailed {
		t.Fatalf("失败状态映射错误: %s", got.Status)
	}
}

func TestRouterDispatch(t *testing.T) {
	suno, voice := NewMock("https://a", 0), NewMock("https://b", 0)
	r := NewRouter(suno).Route(voice, model.KindVoiceCover)

	res, _ := r.Submit(context.Background(), &SubmitRequest{Kind: model.KindVoiceCover, Payload: map[string]interface{}{}})
	if _, ok := voice.tasks[res.ProviderIDs[0]]; !ok {
		t.Error("音色翻唱应分发到音色上游")
	}
	res, _ = r.Submit(context.Background(), &SubmitRequest{Kind: model.KindGenerate, Payload: map[string]interface{}{}})
	if _, ok := suno.tasks[res.ProviderIDs[0]]; !ok {
		t.Error("未登记的类型应走默认上游")
	}
}
