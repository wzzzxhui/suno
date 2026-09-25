package archive

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsMP3(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want bool
	}{
		{"ID3 标签", []byte("ID3\x04\x00"), true},
		{"MPEG1 Layer III 帧", []byte{0xFF, 0xFB, 0x90, 0x64}, true},
		{"AAC ADTS 帧", []byte{0xFF, 0xF1, 0x50, 0x80}, false},
		{"M4A", []byte("\x00\x00\x00\x20ftypM4A "), false},
		{"WAV", []byte("RIFF\x00\x00\x00\x00WAVE"), false},
		{"空", nil, false},
	}
	for _, c := range cases {
		if got := isMP3(c.head); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestTranscodeDeterministic 同一份 M4A 两次转码的结果应逐字节一致，这样下载的文件与证书指纹对得上。
// 本机没有 ffmpeg 时跳过。
func TestTranscodeDeterministic(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("没有 ffmpeg")
	}
	src := filepath.Join(t.TempDir(), "src.m4a")
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=5", "-c:a", "aac", src).CombinedOutput(); err != nil {
		t.Skipf("生成测试音频失败：%v %s", err, out)
	}
	data, _ := os.ReadFile(src)

	m := NewMP3Maker(nil, nil, ffmpeg)
	a, err := m.transcode(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.transcode(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if !isMP3(a) {
		t.Fatalf("转码结果不是 MP3：% x", a[:4])
	}
	if !bytes.Equal(a, b) {
		t.Error("两次转码结果不一致")
	}
}
