package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lepro/suno-open-api/internal/model"
)

// 用本地假服务按文档的请求/响应结构走一遍：提交 → 拆成两条 → 各取各的那首。
func TestMurekaSubmitAndFetch(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/song/generate":
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = io.WriteString(w, `{"id":"1436211","model":"mureka-9.5","status":"preparing"}`)
		case r.URL.Path == "/v1/song/query/1436211":
			_, _ = io.WriteString(w, `{"id":"1436211","model":"mureka-9.5","status":"succeeded","choices":[
				{"index":0,"id":"song-a","url":"https://cdn/a.mp3","duration":152400},
				{"index":1,"id":"song-b","url":"https://cdn/b.mp3","wav_url":"https://cdn/b.wav","duration":150000}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	m := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "key-1", Model: "auto"})
	res, err := m.Submit(context.Background(), &SubmitRequest{Kind: model.KindVoiceSong, Payload: map[string]interface{}{
		"lyrics": "[Verse]\n歌词", "prompt": "pop", "vocal_id": "vocal-1", "title": "歌", "model": "mureka-o2",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ProviderIDs) != 2 || res.ProviderIDs[1] != "1436211#1" {
		t.Fatalf("应拆成两条：%v", res.ProviderIDs)
	}
	if got["vocal_id"] != "vocal-1" || got["lyrics"] != "[Verse]\n歌词" || got["n"] != float64(2) {
		t.Errorf("提交参数不对：%v", got)
	}
	if got["model"] != "auto" {
		t.Errorf("mureka-o2 不支持 vocal_id，应回退到配置的模型：%v", got["model"])
	}
	if _, ok := got["title"]; ok {
		t.Error("title 不是 Mureka 的参数，不应发送")
	}

	r2, err := m.Fetch(context.Background(), &FetchRequest{Kind: model.KindVoiceSong, ProviderID: "1436211#1"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != model.StatusCompleted || r2.CustomID != "song-b" || r2.FileInfo.MP3URL != "https://cdn/b.mp3" ||
		r2.FileInfo.WAVURL != "https://cdn/b.wav" || r2.FileInfo.Duration != 150 {
		t.Errorf("第二首取值不对：%+v %+v", r2, r2.FileInfo)
	}

	bad := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "wrong"})
	if _, err := bad.Submit(context.Background(), &SubmitRequest{Kind: model.KindVoiceSong, Payload: map[string]interface{}{}}); err == nil ||
		!strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("应带出上游错误信息：%v", err)
	}
}

func TestMurekaStatus(t *testing.T) {
	cases := map[string]model.TaskStatus{
		"preparing": model.StatusProcessing, "queued": model.StatusProcessing, "running": model.StatusProcessing,
		"reviewing": model.StatusProcessing, "failed": model.StatusFailed, "timeouted": model.StatusFailed,
		"cancelled": model.StatusFailed,
	}
	for status, want := range cases {
		if got := murekaResult(&murekaSongTask{Status: status}, 0).Status; got != want {
			t.Errorf("%s: got %s, want %s", status, got, want)
		}
	}
	// 成功但没有对应序号的歌，判失败而不是空结果
	if r := murekaResult(&murekaSongTask{Status: "succeeded"}, 1); r.Status != model.StatusFailed {
		t.Errorf("缺少结果应判失败：%+v", r)
	}
}

func TestMurekaCloneVocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/song/vocal-clone" || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(f)
		if string(data) != "ID3fake" || r.FormValue("description") != "我的声音" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"vocal_id":"vocal-abc123","description":"我的声音","created_at":1677610602}`)
	}))
	defer srv.Close()

	id, err := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "k"}).CloneVocal(context.Background(), "我的声音", "v.mp3", []byte("ID3fake"))
	if err != nil || id != "vocal-abc123" {
		t.Fatalf("got %q, %v", id, err)
	}
}

func TestMurekaAdvancedOperations(t *testing.T) {
	tests := []struct {
		name, operation, path, query string
		payload                      map[string]interface{}
		wantKey, wantValue           string
	}{
		{"instrumental", "instrumental", "/v1/instrumental/generate", "/v1/instrumental/query/task-1",
			map[string]interface{}{"prompt": "piano", "model": "auto"}, "prompt", "piano"},
		{"extend", "extend", "/v1/song/extend", "/v1/song/query/task-1",
			map[string]interface{}{"song_id": "song-1", "lyrics": "new words", "extend_at": int64(12000), "extend_type": "tail", "model": "mureka-8"}, "song_id", "song-1"},
		{"remix", "remix", "/v1/song/remix", "/v1/song/query/task-1",
			map[string]interface{}{"upload_audio_id": "file-1", "lyrics": "new words", "prompt": "jazz"}, "upload_audio_id", "file-1"},
		{"easy", "easy", "/v1/song/easy-generate", "/v1/song/query/task-1",
			map[string]interface{}{"prompt": "a pop song", "model": "auto"}, "prompt", "a pop song"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tt.path:
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					_, _ = io.WriteString(w, `{"id":"task-1","status":"preparing"}`)
				case tt.query:
					_, _ = io.WriteString(w, `{"id":"task-1","status":"succeeded","choices":[{"index":0,"id":"song-0","url":"https://cdn/song.mp3","duration":123000},{"index":1,"id":"song-1","url":"https://cdn/song2.mp3","duration":123000}]}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			m := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "test"})
			tt.payload["operation"] = tt.operation
			res, err := m.Submit(context.Background(), &SubmitRequest{Kind: model.KindVoiceSong, Payload: tt.payload})
			if err != nil {
				t.Fatal(err)
			}
			if body[tt.wantKey] != tt.wantValue {
				t.Errorf("body: %+v", body)
			}
			if len(res.ProviderIDs) != 2 {
				t.Fatalf("IDs: %+v", res.ProviderIDs)
			}
			if tt.operation == "instrumental" && !strings.HasPrefix(res.ProviderIDs[0], "instrumental:") {
				t.Errorf("instrumental ID: %s", res.ProviderIDs[0])
			}
			got, err := m.Fetch(context.Background(), &FetchRequest{Kind: model.KindVoiceSong, ProviderID: res.ProviderIDs[1]})
			if err != nil || got.Status != model.StatusCompleted || got.CustomID != "song-1" {
				t.Errorf("fetch: %+v, %v", got, err)
			}
		})
	}
}

func TestMurekaUploadURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files/upload" || r.FormValue("purpose") != "reference" || r.FormValue("url") != "https://cdn/input.mp3" {
			t.Errorf("upload request: %s %s %s", r.URL.Path, r.FormValue("purpose"), r.FormValue("url"))
		}
		_, _ = io.WriteString(w, `{"id":"file-123"}`)
	}))
	defer srv.Close()
	id, err := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "test"}).UploadURL(context.Background(), "reference", "https://cdn/input.mp3")
	if err != nil || id != "file-123" {
		t.Errorf("upload: %q, %v", id, err)
	}
}

func TestMurekaToolAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/video/query/video-task-1" {
			t.Errorf("unexpected tool request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"video-task-1","status":"succeeded"}`)
	}))
	defer srv.Close()
	m := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "test"})
	got, err := m.CallTool(context.Background(), "video_query", "video-task-1", nil)
	if err != nil || !strings.Contains(string(got), "succeeded") {
		t.Errorf("tool response: %s %v", got, err)
	}
	if _, err := m.CallTool(context.Background(), "video_query", "../account/billing", nil); err == nil {
		t.Error("path traversal task ID must be rejected")
	}
	if _, err := m.CallTool(context.Background(), "arbitrary", "", nil); err == nil {
		t.Error("unknown operation must be rejected")
	}
	if _, ok := murekaTools["finetuning_create"]; !ok {
		t.Error("fine-tuning endpoint missing")
	}
}

func TestMurekaBinaryUploads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/files/upload":
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
			}
			if r.FormValue("purpose") != "melody" {
				t.Error("missing purpose")
			}
			if f != nil {
				f.Close()
			}
			_, _ = io.WriteString(w, `{"id":"file-123"}`)
		case "/v1/uploads/add":
			if r.FormValue("upload_id") != "upload-1" {
				t.Error("missing upload ID")
			}
			_, _ = io.WriteString(w, `{"id":"part-1"}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	m := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "test"})
	id, err := m.UploadFile(context.Background(), "melody", "sample.mp3", []byte("audio"))
	if err != nil || id != "file-123" {
		t.Errorf("file: %s %v", id, err)
	}
	part, err := m.UploadPart(context.Background(), "upload-1", "part.mp3", []byte("audio"))
	if err != nil || !strings.Contains(string(part), "part-1") {
		t.Errorf("part: %s %v", part, err)
	}
	if _, err := m.UploadPart(context.Background(), "../invalid", "part.mp3", []byte("audio")); err == nil {
		t.Error("invalid upload ID accepted")
	}
}

func TestMurekaRejectsMockVocalBeforeCallingUpstream(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	m := NewMureka(MurekaConfig{Base: srv.URL, APIKey: "test"})
	_, err := m.Submit(context.Background(), &SubmitRequest{
		Kind:    model.KindVoiceSong,
		Payload: map[string]interface{}{"vocal_id": "mock-vocal-old", "lyrics": "test"},
	})
	if err == nil || !strings.Contains(err.Error(), "模拟音色") {
		t.Fatalf("expected actionable mock voice error, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("invalid voice sent upstream %d times", calls)
	}
}
