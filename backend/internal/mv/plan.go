// Package mv 把一首歌做成长 MV：切分镜、写提示词、逐段生成画面，最后拼接配乐。
package mv

import (
	"math"
	"regexp"
	"strings"

	"github.com/lepro/suno-open-api/internal/model"
)

// Seedance 单段视频时长上下限（秒）；图片镜头沿用同样的切分
const (
	MaxClipSeconds = 12
	MinClipSeconds = 2
)

var (
	sectionTag = regexp.MustCompile(`^\s*\[([^\]]*)\]\s*$`)
	// 段落名里的编号与空白不区分，Chorus 2 与 Chorus 视为同一段落
	sectionNoise = regexp.MustCompile(`[\s\d_\-–—:：#.]+`)
)

// lyricLine 一行歌词：所在段落与该段落第几次出现。
type lyricLine struct {
	Section string
	Occ     int
	Text    string
}

// parseLyrics 解析 Suno 歌词：[Verse] 之类独占一行的标记视为段落名，其余非空行为歌词。
func parseLyrics(raw string) []lyricLine {
	var lines []lyricLine
	section, occ := "", 0
	seen := map[string]int{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := sectionTag.FindStringSubmatch(line); m != nil {
			section = strings.TrimSpace(m[1])
			key := sectionKey(section)
			seen[key]++
			occ = seen[key]
			continue
		}
		lines = append(lines, lyricLine{Section: section, Occ: occ, Text: line})
	}
	return lines
}

func sectionKey(section string) string {
	return strings.ToLower(sectionNoise.ReplaceAllString(section, ""))
}

// isChorus 判断段落是否是副歌 / 高潮。
func isChorus(section string) bool {
	key := sectionKey(section)
	for _, w := range []string{"chorus", "hook", "refrain", "drop", "副歌", "高潮"} {
		if strings.Contains(key, w) {
			return true
		}
	}
	return false
}

// PlanOptions 切分镜的参数。
type PlanOptions struct {
	Tier        string
	ReuseChorus bool
}

// Plan 把歌曲均分为若干段（每段不超过 12 秒），按时间比例分配歌词，
// 再按档位决定每段用图片还是视频，并让重复出现的副歌复用首次副歌的镜头。
func Plan(duration float64, lyrics string, opts PlanOptions) []*model.MVSegment {
	if duration <= 0 {
		return nil
	}
	n := int(math.Ceil(duration / MaxClipSeconds))
	if n < 1 {
		n = 1
	}
	length := duration / float64(n)

	segs := make([]*model.MVSegment, n)
	for i := range segs {
		end := round2(float64(i+1) * length)
		if i == n-1 {
			end = round2(duration)
		}
		segs[i] = &model.MVSegment{Seq: i, Start: round2(float64(i) * length), End: end, Status: model.SegPending}
	}

	// 没有逐字时间轴，按行在全曲上均匀铺开，足够让画面大致跟上歌词
	lines := parseLyrics(lyrics)
	type sectionRef struct {
		name string
		occ  int
	}
	texts := make([][]string, n)
	votes := make([]map[sectionRef]int, n)
	for i, line := range lines {
		idx := i * n / len(lines)
		text := line.Text
		if line.Section != "" && (i == 0 || lines[i-1].Section != line.Section || lines[i-1].Occ != line.Occ) {
			text = "【" + line.Section + "】" + text
		}
		texts[idx] = append(texts[idx], text)
		if votes[idx] == nil {
			votes[idx] = map[sectionRef]int{}
		}
		votes[idx][sectionRef{line.Section, line.Occ}]++
	}

	// 每段归属到其中歌词最多的段落
	refs := make([]sectionRef, n)
	for i := range segs {
		segs[i].Lyrics = strings.Join(texts[i], "\n")
		best := 0
		for ref, count := range votes[i] {
			if count > best || (count == best && ref.occ < refs[i].occ) {
				refs[i], best = ref, count
			}
		}
		segs[i].Section = refs[i].name
	}

	hasChorus := false
	for _, r := range refs {
		if isChorus(r.name) {
			hasChorus = true
			break
		}
	}

	for i, seg := range segs {
		seg.Kind = shotKind(opts.Tier, i, isChorus(refs[i].name), hasChorus)
	}

	if opts.ReuseChorus {
		// 同一副歌每次出现按位置对齐：第 k 次副歌的第 j 个镜头复用首次副歌的第 j 个镜头
		first := map[string][]int{}
		pos := map[sectionRef]int{}
		for i, r := range refs {
			if !isChorus(r.name) {
				continue
			}
			key := sectionKey(r.name)
			if r.occ <= 1 || len(first[key]) == 0 {
				if r.occ <= 1 {
					first[key] = append(first[key], i)
				}
				continue
			}
			j := pos[r]
			pos[r]++
			if j < len(first[key]) {
				src := segs[first[key][j]].Seq
				segs[i].Kind, segs[i].ReuseOf = model.ShotReuse, &src
			}
		}
	}
	return segs
}

// shotKind 按档位决定镜头素材：标准版把视频预算花在开场与副歌上。
func shotKind(tier string, i int, chorus, hasChorus bool) string {
	switch tier {
	case model.MVTierEconomy:
		return model.ShotImage
	case model.MVTierStandard:
		if i == 0 || chorus {
			return model.ShotVideo
		}
		// 歌词没有段落标记时，每三段拍一段视频
		if !hasChorus && i%3 == 1 {
			return model.ShotVideo
		}
		return model.ShotImage
	default:
		return model.ShotVideo
	}
}

// ClipSeconds 返回提交给 Seedance 的整数时长：向上取整，合成时再裁到片段的精确长度。
func ClipSeconds(seg *model.MVSegment) int {
	sec := int(math.Ceil(seg.Length() - 1e-6))
	if sec < MinClipSeconds {
		sec = MinClipSeconds
	}
	if sec > MaxClipSeconds {
		sec = MaxClipSeconds
	}
	return sec
}

// CountShots 统计各类镜头数量，用于展示成本构成。
func CountShots(segs []*model.MVSegment) (videos, images, reused int) {
	for _, s := range segs {
		switch s.Kind {
		case model.ShotVideo:
			videos++
		case model.ShotImage:
			images++
		case model.ShotReuse:
			reused++
		}
	}
	return
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
