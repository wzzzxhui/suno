package certificate

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
)

func TestNewCertificateNo(t *testing.T) {
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.Local)
	pattern := regexp.MustCompile(`^SC20260925-[2-9A-HJ-NP-Z]{8}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		no := newCertificateNo(at)
		if !pattern.MatchString(no) {
			t.Fatalf("编号格式不对：%s", no)
		}
		if seen[no] {
			t.Fatalf("编号重复：%s", no)
		}
		seen[no] = true
	}
}

func TestFillSongInfo(t *testing.T) {
	sunoID := "4852c1a7-cc06-4540-9e88-377ee9ebdefa"
	finished := time.Date(2026, 9, 22, 11, 53, 0, 0, time.Local)
	task := &model.Task{
		CustomID:   &sunoID,
		CreatedAt:  finished.Add(-time.Minute),
		FinishedAt: &finished,
		Extend: `[{"id":"other","title":"另一首"},{"id":"` + sunoID + `","title":"Summer Breeze","model_name":"chirp-hawk",
			"major_model_version":"v6","metadata":{"prompt":"[Verse]\n歌词","tags":"pop","duration":152.4}}]`,
	}

	c := &model.Certificate{SunoID: sunoID}
	fillSongInfo(c, task, `{"title":"提交时的标题","mv":"chirp-hawk"}`)
	if c.Title != "Summer Breeze" || c.ModelName != "v6" || c.Tags != "pop" || c.Duration != 152.4 {
		t.Errorf("取值不对：%+v", c)
	}
	if c.Lyrics != "[Verse]\n歌词" {
		t.Errorf("歌词不对：%q", c.Lyrics)
	}
	if !c.SongCreatedAt.Equal(finished) {
		t.Errorf("创作时间应取完成时间：%v", c.SongCreatedAt)
	}

	// 上游信息缺失时回退到请求参数；纯音乐不带歌词
	c = &model.Certificate{SunoID: sunoID}
	fillSongInfo(c, &model.Task{CustomID: &sunoID},
		`{"title":"请求标题","prompt":"不该出现","tags":"lofi","mv":"chirp-crow","make_instrumental":true}`)
	if c.Title != "请求标题" || c.Lyrics != "" || c.Tags != "lofi" || c.ModelName != "chirp-crow" {
		t.Errorf("回退取值不对：%+v", c)
	}

	c = &model.Certificate{SunoID: sunoID}
	fillSongInfo(c, &model.Task{CustomID: &sunoID}, "")
	if c.Title != "未命名作品" {
		t.Errorf("缺省标题不对：%q", c.Title)
	}
}

func TestCheckSong(t *testing.T) {
	id := "abc"
	ok := &model.Task{Kind: model.KindGenerate, Status: model.StatusCompleted, CustomID: &id}
	if err := checkSong(ok); err != nil {
		t.Errorf("完成的歌曲应可签发：%v", err)
	}
	for _, bad := range []*model.Task{
		{Kind: model.KindGenerate, Status: model.StatusProcessing, CustomID: &id},
		{Kind: model.KindGenerate, Status: model.StatusCompleted},
		{Kind: model.KindVideo, Status: model.StatusCompleted, CustomID: &id},
	} {
		if checkSong(bad) == nil {
			t.Errorf("不应允许签发：%+v", bad)
		}
	}
}

func TestVerifyLink(t *testing.T) {
	s := New(nil, Options{VerifyURL: "https://open.example.com/verify"})
	if got := s.VerifyLink("SC20260925-ABCDEFGH"); got != "https://open.example.com/verify?no=SC20260925-ABCDEFGH" {
		t.Errorf("核验地址不对：%s", got)
	}
	s = New(nil, Options{VerifyURL: "https://open.example.com/verify?lang=zh"})
	if got := s.VerifyLink("X"); got != "https://open.example.com/verify?lang=zh&no=X" {
		t.Errorf("已有参数时应追加：%s", got)
	}
	if New(nil, Options{}).VerifyLink("X") != "" {
		t.Error("未配置核验页时应为空")
	}
}

func TestFileName(t *testing.T) {
	got := FileName(&model.Certificate{No: "SC1", Title: `a/b:c?"d"`})
	if got != "创作证明-abcd-SC1.pdf" {
		t.Errorf("文件名应去掉非法字符：%s", got)
	}
}

func TestGroupHash(t *testing.T) {
	h := strings.Repeat("0123456789abcdef", 4)
	want := "01234567 89abcdef 01234567 89abcdef\n01234567 89abcdef 01234567 89abcdef"
	if got := groupHash(h); got != want {
		t.Errorf("got %q", got)
	}
}

func TestBuildPDF(t *testing.T) {
	jpeg := []byte("\xFF\xD8fake-jpeg\xFF\xD9")
	pdf := buildPDF(jpeg, 10, 20, pdfInfo{Title: "创作证明", Created: time.Date(2026, 9, 25, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))})

	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
		t.Fatal("文件头尾不对")
	}
	if !bytes.Contains(pdf, jpeg) {
		t.Fatal("未嵌入图片")
	}
	if !bytes.Contains(pdf, []byte("/CreationDate (D:20260925080000+08'00')")) {
		t.Error("创建时间格式不对")
	}
	if !bytes.Contains(pdf, []byte(pdfText("创作证明"))) {
		t.Error("标题未按 UTF-16 编码")
	}

	// 交叉引用表里的每个偏移都要正好指向对应对象的开头
	var xref int
	tail := pdf[bytes.LastIndex(pdf, []byte("startxref")):]
	fmt.Sscanf(string(tail), "startxref\n%d", &xref)
	table := strings.Split(string(pdf[xref:]), "\n")
	for i := 1; i <= 6; i++ {
		var off int
		fmt.Sscanf(table[2+i], "%010d", &off)
		if want := fmt.Sprintf("%d 0 obj", i); !bytes.HasPrefix(pdf[off:], []byte(want)) {
			t.Errorf("对象 %d 的偏移 %d 不对", i, off)
		}
	}
}

// TestRenderSample 用本机中文字体画一份样张，找不到字体时跳过。
// 设置 CERT_SAMPLE_DIR 时把 PDF 和 PNG 写到该目录，便于人工检查版面。
func TestRenderSample(t *testing.T) {
	fc := &fontCache{path: os.Getenv("CERT_FONT_FILE")}
	f, err := fc.load()
	if err != nil {
		t.Skipf("没有可用的中文字体：%v", err)
	}

	lyrics := "[Verse 1]\nGolden morning on the kitchen floor\nWaking up to what we've waited for\n\n" +
		strings.Repeat("[Chorus]\n我们摇下车窗一路向前开\n把所有阴影都留在城市外\n吉他的弦和夏天的蓝天\n看云朵慢慢飘过我们眼前\n\n", 4) +
		"[Outro]\nJust the guitar and the sun\nSummer's only just begun"
	c := &model.Certificate{
		No: "SC20260925-7K3QM9XA", SunoID: "4852c1a7-cc06-4540-9e88-377ee9ebdefa",
		Title: "夏日微风 Summer Breeze", Author: "安沐心音乐工作室",
		Lyrics: lyrics, Tags: "bright breezy summer pop, warm acoustic guitar strumming, upbeat organic rhythm, cheerful handclaps, clean sunny male vocals",
		ModelName: "v6", Duration: 152.4, AudioSize: 5632639,
		AudioSHA256:   "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		SongCreatedAt: time.Date(2026, 9, 22, 11, 52, 50, 0, time.Local),
		IssuedAt:      time.Date(2026, 9, 25, 16, 30, 0, 0, time.Local),
	}

	s := &Service{opts: Options{Issuer: "SUNO API 开放平台", VerifyURL: "https://open.example.com/verify"}, fonts: fc}
	start := time.Now()
	pdf, err := s.PDF(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PDF %d KB，耗时 %v", len(pdf)/1024, time.Since(start))
	if again, _ := s.PDF(c); !bytes.Equal(pdf, again) {
		t.Error("同一份证明两次绘制的结果应一致")
	}

	if dir := os.Getenv("CERT_SAMPLE_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "certificate.pdf"), pdf, 0o644)
		img := render(f, c, s.opts.Issuer, s.VerifyLink(c.No), s.opts.VerifyURL)
		out, _ := os.Create(filepath.Join(dir, "certificate.png"))
		_ = png.Encode(out, img)
		out.Close()

		c.Lyrics, c.AudioSize = "", 0
		out, _ = os.Create(filepath.Join(dir, "certificate-instrumental.png"))
		_ = png.Encode(out, render(f, c, "安沐心平台", "", ""))
		out.Close()
	}
}
