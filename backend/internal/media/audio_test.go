package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lepro/suno-open-api/internal/model"
)

// Suno CDN 上加密副本的真实文件头
var encryptedHead = []byte{0x45, 0x51, 0xc4, 0x41, 0x30, 0x13, 0x09, 0xb3, 0xcd, 0xfc, 0x99, 0x0e, 0x4a, 0x11, 0x30, 0x53}

func TestPlayableAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone.m4a":
			http.NotFound(w, r)
		case "/encrypted.m4a":
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(encryptedHead)
		default:
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("ID3\x04\x00\x00\x00\x00\x01oTXXX\x00\x00"))
		}
	}))
	defer srv.Close()

	id := "clip-1"
	task := &model.Task{
		CustomID: &id,
		FileInfo: &model.FileInfo{MP3URL: srv.URL + "/gone.m4a"},
		Extend: `[{"id":"clip-1","audio_url":"` + srv.URL + `/gone.m4a","media_urls":[{"url":"` + srv.URL + `/encrypted.m4a"},{"url":"` + srv.URL + `/ok.mp3"}]},
		          {"id":"other","media_urls":[{"url":"` + srv.URL + `/other.m4a"}]}]`,
	}

	cands := AudioCandidates(task)
	if len(cands) != 3 || cands[0] != srv.URL+"/gone.m4a" {
		t.Fatalf("候选地址应去重且只取本作品: %v", cands)
	}
	// 上游副本已失效、Suno CDN 副本是加密的，都应跳过
	if got := PlayableAudio(context.Background(), task); got != srv.URL+"/ok.mp3" {
		t.Fatalf("应跳过失效与加密的副本: %s", got)
	}
	if got := FirstPlayable(context.Background(), []string{srv.URL + "/gone.m4a", srv.URL + "/encrypted.m4a"}); got != "" {
		t.Fatalf("没有可播放的音频时应返回空: %s", got)
	}
}

func TestIsAudio(t *testing.T) {
	cases := []struct {
		head []byte
		want bool
	}{
		{[]byte("ID3\x04rest"), true},
		{[]byte{0xff, 0xfb, 0x90, 0x00}, true},
		{[]byte("\x00\x00\x00\x20ftypM4A "), true},
		{[]byte("RIFF\x24\x00\x00\x00WAVE"), true},
		{encryptedHead, false},
		{[]byte("<html><head>"), false},
	}
	for _, c := range cases {
		if got := IsAudio(c.head); got != c.want {
			t.Errorf("IsAudio(%q) = %v, want %v", c.head, got, c.want)
		}
	}
}
