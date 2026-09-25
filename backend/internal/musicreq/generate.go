// Package musicreq 承载生成音乐的请求结构与校验，
// 供对外业务接口与运营后台共用，避免两处校验逻辑走偏。
package musicreq

import (
	"strings"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
)

// Generate 是「生成音乐」接口的请求体，覆盖灵感、自定义、延长与翻唱四种模式。
type Generate struct {
	GPTDescriptionPrompt string                 `json:"gpt_description_prompt"`
	Prompt               string                 `json:"prompt"`
	Tags                 string                 `json:"tags"`
	NegativeTags         string                 `json:"negative_tags"`
	MV                   string                 `json:"mv"`
	Title                string                 `json:"title"`
	MakeInstrumental     *bool                  `json:"make_instrumental"`
	Task                 string                 `json:"task"`
	ContinueClipID       string                 `json:"continue_clip_id"`
	ContinueAt           *float64               `json:"continue_at"`
	CoverClipID          string                 `json:"cover_clip_id"`
	Metadata             map[string]interface{} `json:"metadata"`
}

// Payload 校验参数并组装成发往 Provider 的请求体。
func (g *Generate) Payload() (map[string]interface{}, error) {
	if strings.TrimSpace(g.Title) == "" {
		return nil, httpx.BadRequest("缺少必填参数 title")
	}
	if len([]rune(g.Title)) > 100 {
		return nil, httpx.BadRequest("title 最多 100 字符")
	}
	mv, ok := model.NormalizeModel(model.UsageGenerate, g.MV)
	if !ok {
		return nil, httpx.BadRequest("mv 不是受支持的模型版本：" + strings.Join(model.ModelCodes(model.UsageGenerate), "、"))
	}
	if g.MakeInstrumental == nil {
		return nil, httpx.BadRequest("缺少必填参数 make_instrumental")
	}

	// 上游对延长、翻唱同样要求二者至少有一个，提前拦下免得白跑一趟
	if strings.TrimSpace(g.GPTDescriptionPrompt) == "" && strings.TrimSpace(g.Prompt) == "" {
		return nil, httpx.BadRequest("gpt_description_prompt 与 prompt 至少填写一个（延长、翻唱时填写歌词 prompt）")
	}

	switch g.Task {
	case "":
	case "extend":
		if strings.TrimSpace(g.ContinueClipID) == "" {
			return nil, httpx.BadRequest("延长模式需要 continue_clip_id")
		}
	case "cover":
		if strings.TrimSpace(g.CoverClipID) == "" {
			return nil, httpx.BadRequest("翻唱模式需要 cover_clip_id")
		}
	default:
		return nil, httpx.BadRequest("task 仅支持 extend 或 cover")
	}

	payload := map[string]interface{}{
		"mv":                mv,
		"title":             strings.TrimSpace(g.Title),
		"make_instrumental": *g.MakeInstrumental,
	}
	putIfNotEmpty(payload, "gpt_description_prompt", g.GPTDescriptionPrompt)
	putIfNotEmpty(payload, "prompt", g.Prompt)
	putIfNotEmpty(payload, "tags", g.Tags)
	putIfNotEmpty(payload, "negative_tags", g.NegativeTags)
	putIfNotEmpty(payload, "task", g.Task)
	putIfNotEmpty(payload, "continue_clip_id", g.ContinueClipID)
	putIfNotEmpty(payload, "cover_clip_id", g.CoverClipID)
	if g.ContinueAt != nil {
		payload["continue_at"] = *g.ContinueAt
	}
	if len(g.Metadata) > 0 {
		payload["metadata"] = g.Metadata
	}
	return payload, nil
}

func putIfNotEmpty(m map[string]interface{}, key, value string) {
	if strings.TrimSpace(value) != "" {
		m[key] = value
	}
}
