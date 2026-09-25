package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCOSSign(t *testing.T) {
	s := &COSSigner{SecretID: "AKIDtest", SecretKey: "key", Expire: time.Hour}
	now := time.Unix(1700000000, 0)

	signed, err := s.Sign("https://b-1.cos.ap-guangzhou.myqcloud.com/voice_cover/sc_x/out.mp3", now)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(signed)
	q := u.Query()
	if q.Get("q-ak") != "AKIDtest" || q.Get("q-sign-time") != "1700000000;1700003600" || q.Get("q-sign-algorithm") != "sha1" {
		t.Fatalf("签名参数错误: %s", signed)
	}

	// 按规则独立推一遍签名
	keyTime := "1700000000;1700003600"
	want := hmacSHA1Hex(hmacSHA1Hex("key", keyTime),
		"sha1\n"+keyTime+"\n"+sha1Hex("get\n/voice_cover/sc_x/out.mp3\n\n\n")+"\n")
	if q.Get("q-signature") != want || len(want) != 40 {
		t.Fatalf("签名值错误: got %s want %s", q.Get("q-signature"), want)
	}
	if !strings.HasPrefix(signed, "https://b-1.cos.ap-guangzhou.myqcloud.com/voice_cover/sc_x/out.mp3?") {
		t.Fatalf("地址被改动: %s", signed)
	}
}

func TestTMEOutputURLSigned(t *testing.T) {
	tme := NewTME(TMEConfig{OutputBase: "https://b-1.cos.ap-guangzhou.myqcloud.com",
		Signer: &COSSigner{SecretID: "id", SecretKey: "key"}})
	if got := tme.outputURL("/voice_cover/sc_x", "out.mp3"); !strings.Contains(got, "q-signature=") {
		t.Fatalf("配置 COS 密钥后应返回签名地址: %s", got)
	}
	if got := NewTME(TMEConfig{OutputBase: "https://cdn.x"}).outputURL("/d", "a.mp3"); got != "https://cdn.x/d/a.mp3" {
		t.Fatalf("未配置密钥时应返回原地址: %s", got)
	}
}

func TestCOSPut(t *testing.T) {
	var gotAuth, gotType, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth, gotType = r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()

	s := &COSSigner{SecretID: "AKIDtest", SecretKey: "key"}
	if err := s.Put(context.Background(), srv.Client(), srv.URL+"/voice_samples/a.wav", "audio/wav", []byte("RIFF")); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotType != "audio/wav" || gotBody != "RIFF" {
		t.Fatalf("请求错误: %s %s %q", gotMethod, gotType, gotBody)
	}
	// 请求头里是未转义的原文，分号不能被编码
	if !strings.Contains(gotAuth, "q-ak=AKIDtest") || !strings.Contains(gotAuth, "q-sign-time=") ||
		strings.Contains(gotAuth, "%3B") || !strings.Contains(gotAuth, "q-signature=") {
		t.Fatalf("签名头错误: %s", gotAuth)
	}
}

func TestCOSDelete(t *testing.T) {
	status := http.StatusNoContent
	var gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth = r.Method, r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	defer srv.Close()

	s := &COSSigner{SecretID: "AKIDtest", SecretKey: "key"}
	if err := s.Delete(context.Background(), srv.Client(), srv.URL+"/voice_samples/a.wav"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || !strings.Contains(gotAuth, "q-signature=") {
		t.Fatalf("请求错误: %s %s", gotMethod, gotAuth)
	}

	status = http.StatusNotFound
	if err := s.Delete(context.Background(), srv.Client(), srv.URL+"/gone.wav"); err != nil {
		t.Fatalf("对象不存在应视为成功: %v", err)
	}
	status = http.StatusForbidden
	if err := s.Delete(context.Background(), srv.Client(), srv.URL+"/x.wav"); err == nil {
		t.Fatal("无权限时应返回错误")
	}
}
