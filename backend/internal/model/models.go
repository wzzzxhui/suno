package model

import "strings"

// ModelUsage 模型的用途分组。生成、音效与 Remaster 用的是三套不同的代号。
type ModelUsage string

const (
	UsageGenerate ModelUsage = "generate" // 生成音乐（含延长、翻唱）
	UsageSound    ModelUsage = "sound"    // 生成音效
	UsageRemaster ModelUsage = "remaster" // Remaster 升采样
)

// ModelStatus 模型的可用状态。
type ModelStatus string

const (
	StatusActive     ModelStatus = "active"     // 当前主推
	StatusLegacy     ModelStatus = "legacy"     // 仍可调用的旧版本
	StatusDeprecated ModelStatus = "deprecated" // 已下线，调用会被上游自动定向到新版本
)

// MusicModel 一个可选的模型版本。
type MusicModel struct {
	Code    string      `json:"code"`    // 传给接口的 mv / model_name
	Version string      `json:"version"` // 对外版本号，如 V6
	Label   string      `json:"label"`   // 展示名
	Note    string      `json:"note"`    // 补充说明
	Usage   ModelUsage  `json:"usage"`
	Status  ModelStatus `json:"status"`
	Default bool        `json:"default"` // 是否为该用途的默认选项
}

// generateModels 生成音乐可用的模型。
//
// 说明：V6 系列的代号（chirp-hawk / chirp-hawk-wild / chirp-goose）来自平台文档与
// 页面源码，属于内部标签而非官方公开命名；V3 系列自 2026-04 起已下线，
// 传入后会被上游自动定向到较新版本，这里保留以兼容老调用方。
var generateModels = []MusicModel{
	{Code: "chirp-hawk", Version: "V6", Label: "Suno V6", Note: "当前旗舰，综合质量最稳", Usage: UsageGenerate, Status: StatusActive, Default: true},
	{Code: "chirp-hawk-wild", Version: "V6-wild", Label: "Suno V6 Wild", Note: "风格更外放、更实验", Usage: UsageGenerate, Status: StatusActive},
	{Code: "chirp-goose", Version: "V6-mini", Label: "Suno V6 Mini", Note: "更快更轻，适合批量试听", Usage: UsageGenerate, Status: StatusActive},
	{Code: "chirp-fenix", Version: "V5.5", Label: "Suno V5.5 Fenix", Usage: UsageGenerate, Status: StatusLegacy},
	{Code: "chirp-crow", Version: "V5", Label: "Suno V5 Crow", Usage: UsageGenerate, Status: StatusLegacy},
	{Code: "chirp-bluejay", Version: "V4.5+", Label: "Suno V4.5 Plus Bluejay", Usage: UsageGenerate, Status: StatusLegacy},
	{Code: "chirp-auk", Version: "V4.5", Label: "Suno V4.5 Auk", Usage: UsageGenerate, Status: StatusLegacy},
	{Code: "chirp-v4", Version: "V4", Label: "Suno V4", Usage: UsageGenerate, Status: StatusLegacy},
	{Code: "chirp-v3-5", Version: "V3.5", Label: "Suno V3.5", Note: "已下线，调用会被自动定向到较新版本", Usage: UsageGenerate, Status: StatusDeprecated},
	{Code: "chirp-v3-0", Version: "V3", Label: "Suno V3", Note: "已下线，调用会被自动定向到较新版本", Usage: UsageGenerate, Status: StatusDeprecated},
}

// soundModels 生成音效可用的模型。音效走的是 V5 / V5.5 两个代号。
var soundModels = []MusicModel{
	{Code: "chirp-crow", Version: "V5", Label: "Suno V5 Crow", Usage: UsageSound, Status: StatusActive, Default: true},
	{Code: "chirp-fenix", Version: "V5.5", Label: "Suno V5.5 Fenix", Usage: UsageSound, Status: StatusActive},
}

// remasterModels Remaster 升采样用的模型，需与原歌曲版本匹配。
var remasterModels = []MusicModel{
	{Code: "chirp-halibut", Version: "V6", Label: "Remaster V6", Note: "原歌曲为 V6 时使用", Usage: UsageRemaster, Status: StatusActive, Default: true},
	{Code: "chirp-flounder", Version: "V5.5", Label: "Remaster V5.5", Note: "原歌曲为 V5.5 时使用", Usage: UsageRemaster, Status: StatusActive},
	{Code: "chirp-carp", Version: "V5", Label: "Remaster V5", Note: "原歌曲为 V5 时使用", Usage: UsageRemaster, Status: StatusActive},
	{Code: "chirp-bass", Version: "V4.5", Label: "Remaster V4.5", Note: "原歌曲为 V4.5 / V4.5+ 时使用", Usage: UsageRemaster, Status: StatusLegacy},
	{Code: "chirp-up", Version: "V4", Label: "Remaster V4", Note: "原歌曲为 V4 时使用", Usage: UsageRemaster, Status: StatusLegacy},
}

// aliases 兼容不同资料里的写法差异，键为别名，值为标准代号。
var aliases = map[string]string{
	"chirp-v3.5":      "chirp-v3-5",
	"chirp-v3.0":      "chirp-v3-0",
	"chirp-v3-5-0":    "chirp-v3-5",
	"chirp-v5":        "chirp-crow",
	"chirp-v5-5":      "chirp-fenix",
	"chirp-v4-5":      "chirp-auk",
	"chirp-v4-5-plus": "chirp-bluejay",
	"chirp-v6":        "chirp-hawk",
}

// extraModels 由配置注入的额外模型，用于上游发布新版本时免改代码接入。
var extraModels = map[ModelUsage][]MusicModel{}

// RegisterModels 追加一组模型。同一 code 重复注册时后者覆盖前者。
func RegisterModels(usage ModelUsage, models []MusicModel) {
	if len(models) == 0 {
		return
	}
	for i := range models {
		models[i].Usage = usage
		if models[i].Status == "" {
			models[i].Status = StatusActive
		}
	}
	extraModels[usage] = append(extraModels[usage], models...)
}

// ModelsFor 返回某个用途下的全部模型（内置 + 配置注入）。
func ModelsFor(usage ModelUsage) []MusicModel {
	var base []MusicModel
	switch usage {
	case UsageGenerate:
		base = generateModels
	case UsageSound:
		base = soundModels
	case UsageRemaster:
		base = remasterModels
	}

	out := make([]MusicModel, 0, len(base)+len(extraModels[usage]))
	seen := make(map[string]int, len(base))
	for _, m := range base {
		seen[m.Code] = len(out)
		out = append(out, m)
	}
	for _, m := range extraModels[usage] {
		if idx, ok := seen[m.Code]; ok {
			out[idx] = m
			continue
		}
		seen[m.Code] = len(out)
		out = append(out, m)
	}
	return out
}

// NormalizeModel 把别名折算成标准代号，并返回该代号是否受支持。
func NormalizeModel(usage ModelUsage, code string) (string, bool) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", false
	}
	if standard, ok := aliases[strings.ToLower(code)]; ok {
		code = standard
	}
	for _, m := range ModelsFor(usage) {
		if m.Code == code {
			return code, true
		}
	}
	return code, false
}

// ModelCodes 返回某用途下全部可用代号，用于拼错误提示。
func ModelCodes(usage ModelUsage) []string {
	models := ModelsFor(usage)
	codes := make([]string, 0, len(models))
	for _, m := range models {
		codes = append(codes, m.Code)
	}
	return codes
}

// DefaultModel 返回某用途的默认代号。
func DefaultModel(usage ModelUsage) string {
	models := ModelsFor(usage)
	for _, m := range models {
		if m.Default {
			return m.Code
		}
	}
	if len(models) > 0 {
		return models[0].Code
	}
	return ""
}
