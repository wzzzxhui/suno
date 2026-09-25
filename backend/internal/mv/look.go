package mv

import (
	"fmt"
	"strings"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
)

// stylePreset 画风预设：Quality 在提交时追加到每个镜头，Avoid 列出该画风常见的失真。
type stylePreset struct {
	Label   string
	Quality string
	Avoid   string
}

var stylePresets = map[string]stylePreset{
	model.MVStyleRealistic: {
		Label: "真实摄影",
		Quality: "真实摄影照片，全画幅相机 35mm 或 50mm 镜头实拍，自然光与真实环境光，真实的皮肤纹理与毛孔、自然的发丝，" +
			"轻微胶片颗粒，人物比例与神态自然，像专业团队实拍的 MV 画面",
		Avoid: "CG 感、3D 渲染、塑料皮肤、过度磨皮、过度锐化、蜡像感、网红滤镜、畸形手指、多余肢体",
	},
	model.MVStyleCinematic: {
		Label:   "电影剧照",
		Quality: "电影剧照质感，宽银幕构图，电影级调色，体积光与层次分明的明暗，浅景深，真实的人物与场景",
		Avoid:   "廉价滤镜、CG 感、塑料皮肤、过度磨皮、畸形手指",
	},
	model.MVStyleAnime: {
		Label:   "日系动画",
		Quality: "日系动画电影风格，赛璐璐上色，干净利落的线条，细腻的背景美术与通透的光影",
		Avoid:   "写实照片质感、崩坏的五官、线条杂乱",
	},
	model.MVStyleGuofeng: {
		Label:   "国风",
		Quality: "国风美学，东方古典意境，工笔与水墨质感，传统服饰与建筑细节，留白构图",
		Avoid:   "现代元素穿帮、西式建筑、廉价影楼感",
	},
	model.MVStyle3D: {
		Label:   "3D 动画",
		Quality: "3D 动画电影风格，精致建模，柔和的全局光照与次表面散射，角色造型生动",
		Avoid:   "低多边形、贴图模糊、塑料质感",
	},
	model.MVStyleIllustration: {
		Label:   "手绘插画",
		Quality: "手绘插画风格，水彩与彩铅质感，柔和笔触，温暖的绘本氛围",
		Avoid:   "照片质感、3D 渲染",
	},
}

// presetOf 返回画风预设，未设置时按真实摄影处理。
func presetOf(style string) stylePreset {
	if p, ok := stylePresets[style]; ok {
		return p
	}
	return stylePresets[model.MVStyleRealistic]
}

const maxLookField = 200

// NormalizeLook 清理并校验形象设定；参考图只能是该商户上传或生成的图片。
func NormalizeLook(merchantID int64, l model.MVLook) (model.MVLook, error) {
	l.Style = strings.TrimSpace(l.Style)
	if l.Style == "" {
		l.Style = model.MVStyleRealistic
	}
	if _, ok := stylePresets[l.Style]; !ok {
		return l, httpx.BadRequest("不支持的画风：" + l.Style)
	}
	for _, f := range []*string{&l.Character, &l.Outfit, &l.Scene, &l.Palette} {
		*f = strings.TrimSpace(*f)
		if len([]rune(*f)) > maxLookField {
			return l, httpx.BadRequest(fmt.Sprintf("人物、服装、场景、色调每项最多 %d 字", maxLookField))
		}
	}
	prefix := refPrefix(merchantID)
	keys := make([]string, 0, len(l.RefKeys))
	seen := map[string]bool{}
	for _, k := range l.RefKeys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		if !strings.HasPrefix(k, prefix) || strings.Contains(k, "..") {
			return l, httpx.BadRequest("参考图无效，请重新上传")
		}
		seen[k] = true
		keys = append(keys, k)
	}
	if len(keys) > model.MVMaxRefs {
		return l, httpx.BadRequest(fmt.Sprintf("参考图最多 %d 张", model.MVMaxRefs))
	}
	l.RefKeys, l.RefURLs = keys, nil
	return l, nil
}

// refPrefix 参考图在 COS 中的目录，按商户隔离。
func refPrefix(merchantID int64) string { return fmt.Sprintf("mv/refs/m%d/", merchantID) }

// BuildBible 把形象设定写成全片统一设定；画质描述在提交时另外追加，这里只写内容。
func BuildBible(l model.MVLook, styleNote, tags string) string {
	parts := []string{"画风：" + presetOf(l.Style).Label}
	if l.Character != "" {
		parts = append(parts, "主角："+l.Character+"，全片始终是同一个人")
	}
	if l.Outfit != "" {
		parts = append(parts, "服装："+l.Outfit)
	}
	if l.Scene != "" {
		parts = append(parts, "场景："+l.Scene)
	}
	switch {
	case l.Palette != "":
		parts = append(parts, "色调："+l.Palette)
	case tags != "":
		parts = append(parts, fmt.Sprintf("色调与氛围契合「%s」风格的音乐", tags))
	}
	if note := strings.TrimSpace(styleNote); note != "" {
		parts = append(parts, "补充要求："+note)
	}
	return strings.Join(parts, "；")
}

// ComposeImagePrompt 拼出提交给绘画模型的完整提示词：全片设定 + 镜头内容 + 一致性要求 + 画质。
// 早期草稿的镜头提示词里已包含全片设定，不再重复拼接。
func ComposeImagePrompt(bible, shot string, l model.MVLook) string {
	shot = strings.TrimSpace(shot)
	var b strings.Builder
	if bible = strings.TrimSpace(bible); bible != "" && !strings.Contains(shot, bible) {
		b.WriteString(bible)
		b.WriteString("。\n镜头：")
	}
	b.WriteString(strings.TrimRight(shot, "。"))
	b.WriteString("。")
	if l.HasRefs() {
		b.WriteString("\n人物与参考图是同一个人：保持相同的脸型五官、发型发色和服装，只改变姿态、表情与场景。")
	}
	p := presetOf(l.Style)
	fmt.Fprintf(&b, "\n画质：%s。\n避免：%s、画面中的任何文字、字幕、水印与 Logo。", p.Quality, p.Avoid)
	return b.String()
}

// ComposeVideoPrompt 以首帧为起点的视频提示词：人物外貌已由首帧确定，只描述动作与运镜。
func ComposeVideoPrompt(shot string, l model.MVLook) string {
	p := presetOf(l.Style)
	return fmt.Sprintf("%s。%s风格，人物的长相、发型与服装始终与首帧一致，动作自然连贯，不出现文字。",
		strings.TrimRight(strings.TrimSpace(shot), "。"), p.Label)
}

// PortraitPrompt 定妆照提示词：清楚展示主角的脸、发型与服装，供后续每个镜头作参考图。
func PortraitPrompt(l model.MVLook, styleNote string) string {
	bible := BuildBible(l, styleNote, "")
	shot := "主角的定妆照：单人，半身到全身，正面略侧，表情自然，站在简洁干净的背景前，光线均匀柔和，" +
		"清晰展示脸部五官、发型与整套服装的细节"
	if l.Character == "" {
		shot += "；主角的外貌请设计得有辨识度且符合上述设定"
	}
	return ComposeImagePrompt(bible, shot, l)
}
