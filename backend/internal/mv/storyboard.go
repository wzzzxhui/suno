package mv

import (
	"context"
	"fmt"
	"strings"

	"github.com/lepro/suno-open-api/internal/model"
)

// SongInfo 写分镜需要的歌曲信息。
type SongInfo struct {
	Title        string
	Tags         string
	Lyrics       string
	Instrumental bool
	StyleNote    string
	Ratio        string
	Look         model.MVLook
}

// Storyboard 分镜结果：全片统一设定与逐段提示词（按 seq 对应）。
type Storyboard struct {
	VisualBible string
	Prompts     map[int]string
	Writer      string
}

// Writer 负责为分镜片段写画面提示词：LLMWriter 用大模型写，TemplateWriter 为兜底。
// 镜头提示词只写该镜头特有的内容，全片设定与画质在提交时统一拼接。
type Writer interface {
	Write(ctx context.Context, song SongInfo, segs []*model.MVSegment) (*Storyboard, error)
}

/* ---------------------------------- 模板 ---------------------------------- */

// TemplateWriter 不依赖大模型的兜底写法：形象设定 + 歌词情绪 + 轮换景别与机位。
type TemplateWriter struct{}

// 图片镜头的景别构图；运镜在后期完成
var imageShots = []string{
	"中景，主角位于画面三分线处", "面部特写，浅景深，眼神有故事感", "远景，主角在环境中显得渺小",
	"侧面半身，逆光勾出轮廓", "背影，望向远处", "手部与随身物件的细节特写", "低角度仰拍全身", "过肩视角，前景虚化",
}

// 视频镜头的运镜
var videoShots = []string{
	"远景缓慢推近", "中景横向跟拍", "面部特写，浅景深，镜头缓慢推进", "低机位仰拍，缓慢环绕",
	"航拍俯瞰，镜头缓缓下降", "中近景手持跟随", "逆光剪影，镜头缓慢拉远", "细节特写，焦点缓慢转移",
}

// Write 实现 Writer。
func (w *TemplateWriter) Write(ctx context.Context, song SongInfo, segs []*model.MVSegment) (*Storyboard, error) {
	bible := BuildBible(song.Look, song.StyleNote, song.Tags)
	return &Storyboard{VisualBible: bible, Prompts: templatePrompts(segs), Writer: "template"}, nil
}

// templatePrompts 逐段写镜头内容，不含全片设定（提交时统一拼接）。
func templatePrompts(segs []*model.MVSegment) map[int]string {
	out := make(map[int]string, len(segs))
	for _, s := range segs {
		if s.Kind == model.ShotReuse {
			continue
		}
		scene := "没有人物的环境空镜，光影随音乐缓缓流动"
		if lyric := plainLyrics(s.Lyrics); lyric != "" {
			scene = "主角的神情与动作表现这段歌词的情绪与故事：" + lyric
		}
		if s.Kind == model.ShotImage {
			out[s.Seq] = fmt.Sprintf("%s。%s。", scene, imageShots[s.Seq%len(imageShots)])
		} else {
			out[s.Seq] = fmt.Sprintf("%s。%s，动作自然连贯。", scene, videoShots[s.Seq%len(videoShots)])
		}
	}
	return out
}

// plainLyrics 去掉【段落】标记，把多行歌词接成一句。
func plainLyrics(raw string) string {
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		if i := strings.Index(l, "】"); strings.HasPrefix(l, "【") && i > 0 {
			l = l[i+len("】"):]
		}
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "，")
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（无）"
	}
	return s
}
