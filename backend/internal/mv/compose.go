package mv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const fps = 24

// Clip 成片中的一个镜头：素材地址、素材类型与在成片中的时长。
type Clip struct {
	URL    string
	Image  bool // 图片素材做缓慢运镜，否则按视频处理
	Length float64
	Motion int // 图片运镜方式，按镜头序号轮换
}

// Subtitle 一句歌词字幕及其在成片中的起止时间。
type Subtitle struct {
	Start, End float64
	Text       string
}

// SubtitleFile 写入磁盘的一句字幕，ffmpeg 从文件读取文本。
type SubtitleFile struct {
	Path       string
	Start, End float64
}

// Canvas 按画面比例与分辨率档位给出成片尺寸（宽高均为偶数，满足 H.264 要求）。
func Canvas(ratio, resolution string) (int, int) {
	short := map[string]int{"480p": 480, "720p": 720, "1080p": 1080}[resolution]
	if short == 0 {
		short = 720
	}
	w, h := 16, 9
	if parts := strings.Split(ratio, ":"); len(parts) == 2 {
		a, errA := strconv.Atoi(parts[0])
		b, errB := strconv.Atoi(parts[1])
		if errA == nil && errB == nil && a > 0 && b > 0 {
			w, h = a, b
		}
	}
	even := func(v float64) int { return int(v/2) * 2 }
	if w >= h {
		return even(float64(short) * float64(w) / float64(h)), short
	}
	return short, even(float64(short) * float64(h) / float64(w))
}

// imageMotion 四种缓慢运镜：推近、拉远、向右平移、向左平移。
// 素材先放大到两倍再做 zoompan，减少运镜时的像素抖动。
func imageMotion(motion, frames int) string {
	const zoom = 0.12
	center := "x='(iw-iw/zoom)/2':y='(ih-ih/zoom)/2'"
	switch motion % 4 {
	case 0:
		return fmt.Sprintf("z='1+%.2f*on/%d':%s", zoom, frames, center)
	case 1:
		return fmt.Sprintf("z='%.2f-%.2f*on/%d':%s", 1+zoom, zoom, frames, center)
	case 2:
		return fmt.Sprintf("z='%.2f':x='(iw-iw/zoom)*on/%d':y='(ih-ih/zoom)/2'", 1+zoom, frames)
	default:
		return fmt.Sprintf("z='%.2f':x='(iw-iw/zoom)*(1-on/%d)':y='(ih-ih/zoom)/2'", 1+zoom, frames)
	}
}

// ComposeArgs 生成 ffmpeg 参数。
// inputs 为去重后的素材文件，clipInput[i] 指出第 i 个镜头使用哪个输入（复用镜头共享同一输入）。
// 视频镜头统一尺寸与帧率、补齐并裁到计划时长；图片镜头做缓慢运镜；按顺序拼接后配上原曲，可选叠加歌词字幕。
func ComposeArgs(inputs []string, clips []Clip, clipInput []int, audioFile string, duration float64,
	width, height int, subtitles []SubtitleFile, font, out string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	for _, f := range inputs {
		args = append(args, "-i", f)
	}
	audioIndex := len(inputs)
	args = append(args, "-i", audioFile)

	// 同一个输入被多个镜头使用时先 split 成多路
	uses := make(map[int][]int)
	for i, in := range clipInput {
		uses[in] = append(uses[in], i)
	}
	var filter strings.Builder
	source := make([]string, len(clips))
	for in, list := range uses {
		if len(list) == 1 {
			source[list[0]] = fmt.Sprintf("[%d:v]", in)
			continue
		}
		fmt.Fprintf(&filter, "[%d:v]split=%d", in, len(list))
		for _, c := range list {
			source[c] = fmt.Sprintf("[s%d]", c)
			filter.WriteString(source[c])
		}
		filter.WriteString(";")
	}

	for i, c := range clips {
		if c.Image {
			frames := int(c.Length*fps) + 1
			fmt.Fprintf(&filter,
				"%sscale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,"+
					"zoompan=%s:d=%d:s=%dx%d:fps=%d,setsar=1,trim=duration=%.3f,setpts=PTS-STARTPTS[v%d];",
				source[i], width*2, height*2, width*2, height*2, imageMotion(c.Motion, frames), frames,
				width, height, fps, c.Length, i)
			continue
		}
		// 放大后居中裁切铺满画面；片段比计划短时定格最后一帧补足，再裁到精确时长
		fmt.Fprintf(&filter,
			"%sscale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,setsar=1,fps=%d,"+
				"tpad=stop_mode=clone:stop_duration=%d,trim=duration=%.3f,setpts=PTS-STARTPTS[v%d];",
			source[i], width, height, width, height, fps, MaxClipSeconds, c.Length, i)
	}
	for i := range clips {
		fmt.Fprintf(&filter, "[v%d]", i)
	}
	fmt.Fprintf(&filter, "concat=n=%d:v=1:a=0", len(clips))

	// 字幕文本放在文件里，避免歌词里的冒号、引号破坏滤镜语法
	if len(subtitles) > 0 && font != "" {
		filter.WriteString("[cat];[cat]")
		size := height / 16
		for i, sub := range subtitles {
			if i > 0 {
				filter.WriteString(",")
			}
			fmt.Fprintf(&filter,
				"drawtext=fontfile='%s':textfile='%s':fontsize=%d:fontcolor=white:borderw=%d:bordercolor=black@0.7:"+
					"x=(w-text_w)/2:y=h-text_h-%d:enable='%s'",
				filterPath(font), filterPath(sub.Path), size, max(2, size/12), height/12,
				fmt.Sprintf("between(t,%.2f,%.2f)", sub.Start, sub.End))
		}
	}
	filter.WriteString("[v]")

	return append(args,
		"-filter_complex", filter.String(),
		"-map", "[v]", "-map", fmt.Sprintf("%d:a:0", audioIndex),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k",
		"-t", fmt.Sprintf("%.3f", duration),
		"-movflags", "+faststart",
		out,
	)
}

// filterPath 把路径转成 ffmpeg 滤镜参数可用的形式：正斜杠，冒号与引号转义。
func filterPath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.ReplaceAll(p, `'`, `'\''`)
	return strings.ReplaceAll(p, ":", `\:`)
}

// ComposeInput 一次合成需要的全部素材。
type ComposeInput struct {
	FFmpeg    string
	Clips     []Clip
	AudioURL  string
	Duration  float64
	Width     int
	Height    int
	Subtitles []Subtitle
	Font      string
	Out       string
}

// MediaGoneError 某个镜头的素材已无法下载（上游链接过期或被删除），需要重新生成该镜头。
type MediaGoneError struct {
	Clip   int
	Status int
}

func (e *MediaGoneError) Error() string {
	return fmt.Sprintf("第 %d 个镜头素材已失效（HTTP %d）", e.Clip+1, e.Status)
}

// httpStatusError 下载返回非 200 时的状态码。
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// Compose 下载素材（同一地址只下载一次）与音轨，拼接成 MP4 写到 in.Out。
func Compose(ctx context.Context, in ComposeInput) error {
	dir, err := os.MkdirTemp("", "mv-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	client := &http.Client{Timeout: 5 * time.Minute}
	var inputs []string
	index := map[string]int{}
	clipInput := make([]int, len(in.Clips))
	for i, c := range in.Clips {
		if n, ok := index[c.URL]; ok {
			clipInput[i] = n
			continue
		}
		ext := ".mp4"
		if c.Image {
			ext = urlExt(c.URL, ".png")
		}
		file := filepath.Join(dir, fmt.Sprintf("in_%03d%s", len(inputs), ext))
		if err := download(ctx, client, c.URL, file); err != nil {
			// 403/404/410 说明素材链接已失效（上游视频链接 24 小时过期），交给调用方重新生成
			var se *httpStatusError
			if errors.As(err, &se) && (se.code == 403 || se.code == 404 || se.code == 410) {
				return &MediaGoneError{Clip: i, Status: se.code}
			}
			return fmt.Errorf("下载第 %d 个镜头素材失败: %w", i+1, err)
		}
		index[c.URL], clipInput[i] = len(inputs), len(inputs)
		inputs = append(inputs, file)
	}
	audio := filepath.Join(dir, "audio"+urlExt(in.AudioURL, ".mp3"))
	if err := download(ctx, client, in.AudioURL, audio); err != nil {
		return fmt.Errorf("下载音轨失败: %w", err)
	}

	var subFiles []SubtitleFile
	if in.Font != "" {
		for i, sub := range in.Subtitles {
			file := filepath.Join(dir, fmt.Sprintf("sub_%03d.txt", i))
			if err := os.WriteFile(file, []byte(sub.Text), 0o644); err != nil {
				return err
			}
			subFiles = append(subFiles, SubtitleFile{Path: file, Start: sub.Start, End: sub.End})
		}
	}

	args := ComposeArgs(inputs, in.Clips, clipInput, audio, in.Duration, in.Width, in.Height, subFiles, in.Font, in.Out)
	cmd := exec.CommandContext(ctx, in.FFmpeg, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(output))
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		return fmt.Errorf("ffmpeg 合成失败: %v %s", err, msg)
	}
	return nil
}

// BuildSubtitles 把每个镜头的歌词在镜头时长内均分，得到逐句字幕。
func BuildSubtitles(segs []SubtitleSource) []Subtitle {
	var out []Subtitle
	for _, s := range segs {
		var lines []string
		for _, l := range strings.Split(s.Lyrics, "\n") {
			// 去掉切分时加上的【段落】标记
			if i := strings.Index(l, "】"); strings.HasPrefix(l, "【") && i > 0 {
				l = l[i+len("】"):]
			}
			if l = strings.TrimSpace(l); l != "" {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			continue
		}
		step := (s.End - s.Start) / float64(len(lines))
		for i, l := range lines {
			out = append(out, Subtitle{Start: s.Start + step*float64(i), End: s.Start + step*float64(i+1), Text: l})
		}
	}
	return out
}

// SubtitleSource 生成字幕所需的镜头信息。
type SubtitleSource struct {
	Start, End float64
	Lyrics     string
}

func download(ctx context.Context, client *http.Client, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{code: resp.StatusCode}
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func urlExt(url, fallback string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		url = url[:i]
	}
	if ext := strings.ToLower(filepath.Ext(url)); ext != "" && len(ext) <= 5 {
		return ext
	}
	return fallback
}
