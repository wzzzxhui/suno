package mv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lepro/suno-open-api/internal/model"
)

// 典型的 Suno 歌词结构：副歌出现两次
const lyrics = `[Verse 1]
第一句
第二句
第三句
第四句
[Chorus]
副歌一
副歌二
副歌三
副歌四
[Verse 2]
第五句
第六句
第七句
第八句
[Chorus]
副歌一
副歌二
副歌三
副歌四`

func TestPlan(t *testing.T) {
	segs := Plan(150.4, lyrics, PlanOptions{Tier: model.MVTierPremium})
	// 150.4 秒至少 13 段才能每段不超过 12 秒，且均分不留碎片
	if len(segs) != 13 {
		t.Fatalf("段数 = %d, want 13", len(segs))
	}
	for i, s := range segs {
		if s.Seq != i || s.Length() > MaxClipSeconds+0.01 || s.Length() < 11 {
			t.Fatalf("第 %d 段时长异常: %.2f", i, s.Length())
		}
		if i > 0 && s.Start != segs[i-1].End {
			t.Fatalf("第 %d 段与上一段不衔接", i)
		}
		if s.Kind != model.ShotVideo {
			t.Fatalf("高级版应全部是视频: %s", s.Kind)
		}
	}
	if segs[len(segs)-1].End != 150.4 {
		t.Fatalf("最后一段应结束于歌曲末尾: %.2f", segs[len(segs)-1].End)
	}

	var all []string
	for _, s := range segs {
		all = append(all, s.Lyrics)
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{"【Verse 1】第一句", "第四句", "【Chorus】副歌一", "【Verse 2】第五句"} {
		if !strings.Contains(joined, want) {
			t.Errorf("歌词分配缺少 %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "[Verse") {
		t.Error("段落标记不应作为歌词行")
	}

	if got := Plan(0, lyrics, PlanOptions{}); got != nil {
		t.Error("时长为 0 时不应切分")
	}
	if got := Plan(8, "", PlanOptions{}); len(got) != 1 || got[0].End != 8 {
		t.Errorf("短歌应只有一段: %+v", got)
	}
}

func TestPlanTiers(t *testing.T) {
	eco := Plan(96, lyrics, PlanOptions{Tier: model.MVTierEconomy})
	if v, i, _ := CountShots(eco); v != 0 || i != len(eco) {
		t.Fatalf("经济版应全部是图片: 视频 %d 图片 %d", v, i)
	}

	std := Plan(96, lyrics, PlanOptions{Tier: model.MVTierStandard})
	if std[0].Kind != model.ShotVideo {
		t.Error("标准版开场应是视频")
	}
	for _, s := range std {
		if s.Seq == 0 {
			continue
		}
		want := model.ShotImage
		if isChorus(s.Section) {
			want = model.ShotVideo
		}
		if s.Kind != want {
			t.Errorf("标准版第 %d 段（%s）应为 %s，实际 %s", s.Seq, s.Section, want, s.Kind)
		}
	}
	if v, i, _ := CountShots(std); v == 0 || i == 0 {
		t.Fatalf("标准版应同时有图片与视频: 视频 %d 图片 %d", v, i)
	}

	// 没有段落标记时每三段一段视频
	plain := Plan(96, "一\n二\n三\n四\n五\n六\n七\n八", PlanOptions{Tier: model.MVTierStandard})
	if v, _, _ := CountShots(plain); v < 2 || v > 4 {
		t.Fatalf("无段落标记时视频镜头数不合理: %d", v)
	}
}

func TestPlanReuseChorus(t *testing.T) {
	// 96 秒 8 段，20 行歌词：每段约 2.5 行，两次副歌各占约 2 段
	segs := Plan(96, lyrics, PlanOptions{Tier: model.MVTierPremium, ReuseChorus: true})
	_, _, reused := CountShots(segs)
	if reused == 0 {
		t.Fatalf("第二次副歌应复用镜头: %+v", sections(segs))
	}
	for _, s := range segs {
		if s.Kind != model.ShotReuse {
			continue
		}
		if s.ReuseOf == nil || *s.ReuseOf >= s.Seq {
			t.Fatalf("第 %d 段应复用更早的镜头: %v", s.Seq, s.ReuseOf)
		}
		src := segs[*s.ReuseOf]
		if !isChorus(src.Section) || src.Kind == model.ShotReuse {
			t.Fatalf("第 %d 段复用的第 %d 段不是首次副歌", s.Seq, src.Seq)
		}
	}

	// 不开复用时不应出现复用镜头
	if _, _, r := CountShots(Plan(96, lyrics, PlanOptions{Tier: model.MVTierPremium})); r != 0 {
		t.Fatal("未开启复用却出现了复用镜头")
	}
}

func sections(segs []*model.MVSegment) []string {
	var out []string
	for _, s := range segs {
		out = append(out, fmt.Sprintf("%d:%s/%s", s.Seq, s.Section, s.Kind))
	}
	return out
}

func TestClipSeconds(t *testing.T) {
	cases := map[float64]int{11.57: 12, 12: 12, 1.2: 2, 8: 8, 30: 12}
	for length, want := range cases {
		if got := ClipSeconds(&model.MVSegment{End: length}); got != want {
			t.Errorf("ClipSeconds(%.2f) = %d, want %d", length, got, want)
		}
	}
}

func TestCanvas(t *testing.T) {
	cases := []struct {
		ratio, res string
		w, h       int
	}{
		{"16:9", "720p", 1280, 720},
		{"9:16", "720p", 720, 1280},
		{"1:1", "1080p", 1080, 1080},
		{"4:3", "480p", 640, 480},
		{"bad", "", 1280, 720},
	}
	for _, c := range cases {
		if w, h := Canvas(c.ratio, c.res); w != c.w || h != c.h {
			t.Errorf("Canvas(%s,%s) = %dx%d, want %dx%d", c.ratio, c.res, w, h, c.w, c.h)
		}
	}
}

func TestComposeArgs(t *testing.T) {
	clips := []Clip{{Length: 11.5}, {Image: true, Length: 9.25, Motion: 2}, {Length: 4}}
	// 第三个镜头复用第一个输入
	args := ComposeArgs([]string{"a.mp4", "b.png"}, clips, []int{0, 1, 0}, "song.mp3", 24.75, 1280, 720,
		[]SubtitleFile{{Path: `C:\tmp\sub_000.txt`, Start: 0, End: 3}}, `C:\Windows\Fonts\msyh.ttc`, "out.mp4")
	line := strings.Join(args, " ")
	for _, want := range []string{
		"-i a.mp4 -i b.png -i song.mp3",
		"[0:v]split=2[s0][s2]",
		"trim=duration=11.500", "trim=duration=4.000",
		"zoompan=z='1.12':x='(iw-iw/zoom)*on/223'",
		"[v0][v1][v2]concat=n=3:v=1:a=0[cat];[cat]drawtext=",
		`fontfile='C\:/Windows/Fonts/msyh.ttc'`, `textfile='C\:/tmp/sub_000.txt'`,
		"enable='between(t,0.00,3.00)'",
		"-map 2:a:0", "-t 24.750", "out.mp4",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("参数缺少 %q:\n%s", want, line)
		}
	}
}

func TestBuildSubtitles(t *testing.T) {
	subs := BuildSubtitles([]SubtitleSource{
		{Start: 0, End: 12, Lyrics: "【Verse 1】第一句\n第二句\n第三句"},
		{Start: 12, End: 24},
	})
	if len(subs) != 3 {
		t.Fatalf("字幕条数 = %d, want 3", len(subs))
	}
	if subs[0].Text != "第一句" || subs[0].Start != 0 || subs[0].End != 4 || subs[2].End != 12 {
		t.Fatalf("字幕切分错误: %+v", subs)
	}
}

func TestTemplateWriter(t *testing.T) {
	segs := Plan(30, lyrics, PlanOptions{Tier: model.MVTierStandard})
	look := model.MVLook{Style: model.MVStyleRealistic, Character: "二十岁女生，齐肩黑发", Outfit: "白色连衣裙"}
	sb, err := (&TemplateWriter{}).Write(context.Background(),
		SongInfo{Title: "夏日", Tags: "pop", StyleNote: "赛博朋克夜景", Look: look}, segs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"赛博朋克夜景", "真实摄影", "齐肩黑发", "白色连衣裙"} {
		if !strings.Contains(sb.VisualBible, want) {
			t.Errorf("整体设定缺少 %q: %s", want, sb.VisualBible)
		}
	}
	if sb.Writer != "template" {
		t.Fatalf("writer = %s", sb.Writer)
	}
	for _, s := range segs {
		p := sb.Prompts[s.Seq]
		// 镜头提示词只写镜头内容，全片设定在提交时拼接；段落标记不能进提示词
		if p == "" || strings.Contains(p, sb.VisualBible) || strings.Contains(p, "【") {
			t.Errorf("第 %d 段提示词不对: %s", s.Seq, p)
		}
	}
}

// 本机装了 ffmpeg 时，用生成的测试素材跑一遍真实合成：视频、图片运镜、复用镜头与歌词字幕。
func TestComposeWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("未安装 ffmpeg")
	}
	dir := t.TempDir()
	gen := func(name string, args ...string) {
		if out, err := exec.Command(ffmpeg, append([]string{"-y", "-loglevel", "error"}, append(args, filepath.Join(dir, name))...)...).CombinedOutput(); err != nil {
			t.Fatalf("生成素材失败: %v %s", err, out)
		}
	}
	// 视频比计划短，用来验证补帧；图片为方形，用来验证裁切铺满
	gen("a.mp4", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=30:duration=1.5")
	gen("b.png", "-f", "lavfi", "-i", "testsrc=size=800x800", "-frames:v", "1")
	gen("song.mp3", "-f", "lavfi", "-i", "sine=frequency=440:duration=6")

	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()

	font := ""
	for _, f := range []string{`C:\Windows\Fonts\msyh.ttc`, "/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"} {
		if _, err := os.Stat(f); err == nil {
			font = f
			break
		}
	}

	out := filepath.Join(dir, "out.mp4")
	err = Compose(context.Background(), ComposeInput{
		FFmpeg: ffmpeg,
		Clips: []Clip{
			{URL: srv.URL + "/a.mp4", Length: 2},
			{URL: srv.URL + "/b.png", Image: true, Length: 2, Motion: 0},
			{URL: srv.URL + "/a.mp4", Length: 2}, // 复用第一个镜头
		},
		AudioURL:  srv.URL + "/song.mp3",
		Duration:  6,
		Width:     640,
		Height:    360,
		Subtitles: []Subtitle{{Start: 0, End: 2, Text: "测试字幕：一句歌词"}, {Start: 2, End: 4, Text: "第二句"}},
		Font:      font,
		Out:       out,
	})
	if err != nil {
		t.Fatal(err)
	}

	probe, err := exec.Command(filepath.Join(filepath.Dir(ffmpeg), "ffprobe"+filepath.Ext(ffmpeg)),
		"-v", "error", "-show_entries", "format=duration:stream=width,height,codec_type", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatalf("ffprobe 失败: %v", err)
	}
	info := string(probe)
	if !strings.Contains(info, "640,360") || !strings.Contains(info, "audio") {
		t.Fatalf("成片规格不对: %s", info)
	}
	var dur float64
	lines := strings.Split(strings.TrimSpace(info), "\n")
	fmt.Sscanf(lines[len(lines)-1], "%f", &dur)
	if dur < 5.8 || dur > 6.3 {
		t.Fatalf("成片时长应约 6 秒: %.2f", dur)
	}
	t.Logf("成片 %.2f 秒，字幕字体：%q", dur, font)
}

func TestComposeMediaGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/expired.mp4") {
			http.Error(w, "AccessDenied", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	err := Compose(context.Background(), ComposeInput{
		FFmpeg:   "ffmpeg",
		Clips:    []Clip{{URL: srv.URL + "/ok.mp4", Length: 2}, {URL: srv.URL + "/expired.mp4", Length: 2}},
		AudioURL: srv.URL + "/song.mp3",
		Duration: 4, Width: 640, Height: 360, Out: filepath.Join(t.TempDir(), "out.mp4"),
	})
	var gone *MediaGoneError
	if !errors.As(err, &gone) || gone.Clip != 1 || gone.Status != http.StatusForbidden {
		t.Fatalf("过期素材应返回 MediaGoneError(第 2 个镜头): %v", err)
	}
}

func TestResolveFont(t *testing.T) {
	f := filepath.Join(t.TempDir(), "font.ttc")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveFont(f); got != f {
		t.Fatalf("配置的字体存在时应直接使用: %s", got)
	}
	// 配置的路径不存在（如把 Windows 路径带到 Linux）时，退回到系统常见位置；本机没有则为空
	got := ResolveFont("/no/such/font.ttc")
	if got != "" {
		if _, err := os.Stat(got); err != nil {
			t.Fatalf("回退的字体应真实存在: %s", got)
		}
	}
}

func TestCheckFFmpeg(t *testing.T) {
	if got := CheckFFmpeg("definitely-not-ffmpeg"); len(got) != 1 || !strings.Contains(got[0], "未找到") {
		t.Fatalf("找不到 ffmpeg 时应提示: %v", got)
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		if missing := CheckFFmpeg("ffmpeg"); len(missing) > 0 {
			t.Fatalf("本机 ffmpeg 缺少合成所需能力: %v", missing)
		}
	}
}
