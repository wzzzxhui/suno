package mv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lepro/suno-open-api/internal/model"
)

// LLMWriter 用兼容 OpenAI Chat Completions 的大模型写分镜（DeepSeek、通义千问、豆包、GPT 等均可）。
type LLMWriter struct {
	BaseURL string // 如 https://api.deepseek.com/v1
	APIKey  string
	Model   string
	Client  *http.Client
}

const llmSystemPrompt = `你是经验丰富的 MV 导演兼分镜师。根据歌曲信息、运营给定的形象设定和镜头列表，写出整支 MV 的统一设定与逐个镜头的画面描述，交给 AI 绘画 / 视频模型生成。

要求：
1. visual_bible：全片统一设定，不超过 300 字，依次写明：画风（必须使用给定画风）、主角固定的外貌（性别、年龄、脸型五官、发型发色、身材）、主角固定的服装、主要场景、色调与光线。运营已给出的设定必须保留原意并写得更具体；没给出的由你根据歌曲设计，要具体、有辨识度，不能写"某人""一个人"这种模糊描述。除非歌词明确需要，全片只有一位主角。
2. 每个镜头的 prompt 不超过 150 字，只写这个镜头特有的内容：主角在哪里、在做什么、表情神态、景别构图、光线。用"主角"指代，不要重复 visual_bible 里的外貌和服装（系统会自动拼接）。
3. 把歌词转成具体可拍的情节和动作，不要引用歌词原文，不要写"意境""象征""表现情绪"之类抽象词。前后镜头要能串成一个连贯的故事，场景可以变化但属于同一个世界；副歌的画面情绪更强烈，前奏、间奏、尾奏可以用环境空镜或主角的背影。
4. 图片镜头描述一个静止瞬间，像真实 MV 的截图；视频镜头描述约 10 秒内的一个连续动作，并指定一种运镜（推、拉、摇、移、跟、环绕之一）。
5. 画风为真实摄影或电影剧照时，写得像真实拍摄：具体的地点、真实的道具、自然的动作与光线，不要奇幻夸张的元素，除非歌词需要。
6. 画面里不能出现文字、字幕、Logo。
7. 只输出 JSON，格式：{"visual_bible":"...","shots":[{"seq":0,"prompt":"..."}]}，shots 覆盖镜头列表里的每一个序号。`

// Write 实现 Writer。
func (w *LLMWriter) Write(ctx context.Context, song SongInfo, segs []*model.MVSegment) (*Storyboard, error) {
	content, err := w.chat(ctx, llmSystemPrompt, llmUserPrompt(song, segs))
	if err != nil {
		return nil, err
	}
	var out struct {
		VisualBible string `json:"visual_bible"`
		Shots       []struct {
			Seq    int    `json:"seq"`
			Prompt string `json:"prompt"`
		} `json:"shots"`
	}
	if err := json.Unmarshal([]byte(extractJSON(content)), &out); err != nil {
		return nil, fmt.Errorf("大模型返回的分镜不是有效 JSON: %w", err)
	}

	prompts := make(map[int]string, len(out.Shots))
	for _, s := range out.Shots {
		if p := strings.TrimSpace(s.Prompt); p != "" {
			prompts[s.Seq] = truncateRunes(p, 800)
		}
	}
	// 漏写的镜头用模板补上，漏得太多说明回答不可靠，整体退回模板
	fallback := templatePrompts(segs)
	missing := 0
	for seq, p := range fallback {
		if _, ok := prompts[seq]; !ok {
			prompts[seq] = p
			missing++
		}
	}
	if len(fallback) > 0 && missing*2 > len(fallback) {
		return nil, fmt.Errorf("大模型只写了 %d/%d 个镜头", len(fallback)-missing, len(fallback))
	}
	for seq := range prompts {
		if _, ok := fallback[seq]; !ok {
			delete(prompts, seq) // 复用镜头或不存在的序号
		}
	}

	bible := strings.TrimSpace(out.VisualBible)
	if bible == "" {
		bible = BuildBible(song.Look, song.StyleNote, song.Tags)
	}
	return &Storyboard{VisualBible: truncateRunes(bible, 1000), Prompts: prompts, Writer: "llm"}, nil
}

// llmUserPrompt 歌曲信息、形象设定与镜头列表。
func llmUserPrompt(song SongInfo, segs []*model.MVSegment) string {
	l := song.Look
	var b strings.Builder
	fmt.Fprintf(&b, "歌名：%s\n音乐风格：%s\n画面比例：%s\n", song.Title, orNone(song.Tags), orNone(song.Ratio))
	if song.Instrumental {
		b.WriteString("这是纯音乐，没有歌词，请根据音乐风格设计画面。\n")
	}
	b.WriteString("\n形象设定（运营填写，留空的由你设计）：\n")
	fmt.Fprintf(&b, "- 画风：%s\n- 主角外貌：%s\n- 服装：%s\n- 场景：%s\n- 色调：%s\n- 补充要求：%s\n",
		presetOf(l.Style).Label, orNone(l.Character), orNone(l.Outfit), orNone(l.Scene), orNone(l.Palette),
		orNone(song.StyleNote))
	if l.HasRefs() {
		b.WriteString("- 运营提供了主角的参考照片，生成时会作为参考图；visual_bible 里的外貌描述不要与之冲突，未填写的外貌项写得概括一些即可。\n")
	}

	b.WriteString("\n镜头列表：\n")
	for _, s := range segs {
		if s.Kind == model.ShotReuse {
			continue
		}
		kind := "视频"
		if s.Kind == model.ShotImage {
			kind = "图片"
		}
		lyric := plainLyrics(s.Lyrics)
		if lyric == "" {
			lyric = "（无歌词：前奏 / 间奏 / 尾奏）"
		}
		fmt.Fprintf(&b, "#%d [%s] %s-%s 段落：%s 歌词：%s\n", s.Seq, kind, clockOf(s.Start), clockOf(s.End),
			orNone(s.Section), lyric)
	}
	return b.String()
}

func (w *LLMWriter) chat(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": w.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0.8,
		"response_format": map[string]string{"type": "json_object"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(w.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.APIKey)

	client := w.Client
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("大模型接口返回 HTTP %d: %s", resp.StatusCode, truncateRunes(string(raw), 300))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("大模型接口返回格式异常: %s", truncateRunes(string(raw), 300))
	}
	return out.Choices[0].Message.Content, nil
}

// extractJSON 取出回答中的 JSON 对象，兼容包在 ```json 代码块里或前后带说明文字的情况。
func extractJSON(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func clockOf(sec float64) string {
	total := int(sec + 0.5)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}
