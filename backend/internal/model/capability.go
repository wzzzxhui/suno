package model

// 能力分组
const (
	GroupMusic = "music"
	GroupImage = "image"
	GroupVideo = "video"
)

// 图片与视频类任务
const (
	KindImageSeedream45    TaskKind = "image_seedream_45"
	KindImageSeedream50    TaskKind = "image_seedream_50"
	KindImageSeedream50Pro TaskKind = "image_seedream_50_pro"
	KindImageSeedreamFour  TaskKind = "image_seedream_four"
	KindImageNano          TaskKind = "image_nano"
	KindImageNanoPro       TaskKind = "image_nano_pro"
	KindImageNano2         TaskKind = "image_nano2"
	KindImageGPT           TaskKind = "image_gpt"

	KindVideoSeedance   TaskKind = "video_seedance"
	KindVideoSeedance25 TaskKind = "video_seedance_25"
	KindVideoVeo        TaskKind = "video_veo"
	KindVideoGrok       TaskKind = "video_grok"
	KindVideoMiniMaxH3  TaskKind = "video_minimax_h3"
)

// CapParam 描述一个可在后台表单里填写的参数。
type CapParam struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // string | number | boolean | array | object
	Required    bool        `json:"required"`
	Label       string      `json:"label"`
	Description string      `json:"description"`
	Options     []string    `json:"options,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Multiline   bool        `json:"multiline,omitempty"`
}

// Capability 一项生成能力：提交路径、查询路径、定价与表单参数。
type Capability struct {
	Kind   TaskKind `json:"kind"`
	Group  string   `json:"group"`
	Vendor string   `json:"vendor"`
	Label  string   `json:"label"`
	Desc   string   `json:"desc"`

	SubmitPath string `json:"submit_path"`
	// QueryPath 为空时走音乐任务查询接口
	QueryPath string `json:"query_path"`

	Price   int64      `json:"price"`
	Outputs int        `json:"outputs"`
	Params  []CapParam `json:"params"`
}

// 各任务查询路径
const (
	queryDraw     = "/api/v2/draw/task"
	queryNano     = "/api/v2/nano/task"
	queryGPTImage = "/api/v2/gpt-image/task"
	queryVideo    = "/api/v2/video/task"
	queryVeo      = "/api/v2/veo/task"
	queryGrok     = "/api/v2/video/grok/task"
)

// 表单里反复用到的参数
func promptParam(desc, def string) CapParam {
	return CapParam{Name: "prompt", Type: "string", Required: true, Label: "提示词",
		Description: desc, Default: def, Multiline: true}
}

func referenceImagesParam(desc string) CapParam {
	return CapParam{Name: "reference_images", Type: "array", Label: "参考图片",
		Description: desc}
}

// imageCapabilities 图片生成能力。
var imageCapabilities = []Capability{
	{
		Kind: KindImageSeedream50Pro, Group: GroupImage, Vendor: "即梦", Label: "即梦绘画 5.0 Pro",
		Desc:       "seedream-5.0-pro，支持 2K/4K 超清与参考图",
		SubmitPath: "/api/v2/draw-5-0-pro", QueryPath: queryDraw, Price: 20, Outputs: 1,
		Params: []CapParam{
			promptParam("描述想要的画面；开启图层拆分时可省略", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			referenceImagesParam("参考图片 URL 数组，可留空"),
			{Name: "size", Type: "string", Label: "尺寸", Description: "1K / 1.5K / 2K，或宽x高（如 2048x1024）",
				Options: []string{"1K", "1.5K", "2K"}, Default: "2K"},
			{Name: "model", Type: "string", Label: "模型", Description: "留空用默认模型"},
		},
	},
	{
		Kind: KindImageSeedream50, Group: GroupImage, Vendor: "即梦", Label: "即梦绘画 5.0",
		Desc:       "seedream-5.0，支持 2K/4K 超清生成",
		SubmitPath: "/api/v2/draw-5-0", QueryPath: queryDraw, Price: 15, Outputs: 1,
		Params: []CapParam{
			promptParam("描述想要的画面", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			referenceImagesParam("参考图片 URL 数组，可留空"),
			{Name: "size", Type: "string", Label: "尺寸", Options: []string{"1K", "1.5K", "2K", "4K"}, Default: "2K"},
			{Name: "web_search", Type: "boolean", Label: "联网搜索", Description: "开启会增加时延"},
			{Name: "output_format", Type: "string", Label: "输出格式", Options: []string{"png", "jpeg", "webp"}},
		},
	},
	{
		Kind: KindImageSeedream45, Group: GroupImage, Vendor: "即梦", Label: "即梦绘画 4.5",
		Desc:       "seedream-4.5，稳定通用的绘画模型",
		SubmitPath: "/api/v2/draw-4-5", QueryPath: queryDraw, Price: 10, Outputs: 1,
		Params: []CapParam{
			promptParam("描述想要的画面", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			referenceImagesParam("参考图片 URL 数组，可留空"),
			{Name: "size", Type: "string", Label: "尺寸", Options: []string{"1K", "1.5K", "2K"}, Default: "2K"},
			{Name: "web_search", Type: "boolean", Label: "联网搜索"},
			{Name: "output_format", Type: "string", Label: "输出格式", Options: []string{"png", "jpeg", "webp"}},
		},
	},
	{
		Kind: KindImageSeedreamFour, Group: GroupImage, Vendor: "即梦", Label: "即梦绘画 4.5（四图）",
		Desc:       "一次产出四张候选图",
		SubmitPath: "/api/v2/draw-4-5-four-images", QueryPath: queryDraw, Price: 30, Outputs: 1,
		Params: []CapParam{
			promptParam("描述想要的画面", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			referenceImagesParam("参考图片 URL 数组，可留空"),
			{Name: "size", Type: "string", Label: "尺寸", Options: []string{"1K", "1.5K", "2K"}, Default: "2K"},
		},
	},
	{
		Kind: KindImageNano2, Group: GroupImage, Vendor: "NANO", Label: "Nano Banana 2",
		Desc:       "gemini-3.1-flash-image-preview",
		SubmitPath: "/api/v2/nano2", QueryPath: queryNano, Price: 12, Outputs: 1,
		Params: []CapParam{
			promptParam("文本提示词", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			{Name: "image_size", Type: "string", Label: "分辨率", Options: []string{"1K", "2K", "4K"}},
			{Name: "aspect_ratio", Type: "string", Label: "宽高比",
				Options: []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}, Default: "1:1"},
			referenceImagesParam("参考图片 URL 数组，最多 10 张"),
		},
	},
	{
		Kind: KindImageNanoPro, Group: GroupImage, Vendor: "NANO", Label: "Nano Banana Pro",
		Desc:       "Gemini 3 Pro 高质量出图",
		SubmitPath: "/api/v2/nano-pro", QueryPath: queryNano, Price: 25, Outputs: 1,
		Params: []CapParam{
			promptParam("文本提示词", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			{Name: "image_size", Type: "string", Label: "分辨率", Options: []string{"1K", "2K", "4K"}},
			{Name: "aspect_ratio", Type: "string", Label: "宽高比",
				Options: []string{"1:1", "16:9", "9:16", "4:3", "3:4"}, Default: "1:1"},
			referenceImagesParam("参考图片 URL 数组，最多 10 张"),
		},
	},
	{
		Kind: KindImageNano, Group: GroupImage, Vendor: "NANO", Label: "Nano Banana 标准",
		Desc:       "标准绘画模型，速度快",
		SubmitPath: "/api/v2/nano", QueryPath: queryNano, Price: 8, Outputs: 1,
		Params: []CapParam{
			promptParam("文本提示词", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			{Name: "aspect_ratio", Type: "string", Label: "宽高比",
				Options: []string{"1:1", "16:9", "9:16", "4:3", "3:4"}, Default: "1:1"},
			referenceImagesParam("参考图片 URL 数组"),
		},
	},
	{
		Kind: KindImageGPT, Group: GroupImage, Vendor: "GPT", Label: "GPT Image 2",
		Desc:       "支持生成与编辑，可指定渲染质量",
		SubmitPath: "/api/v2/gpt-image-2", QueryPath: queryGPTImage, Price: 25, Outputs: 1,
		Params: []CapParam{
			promptParam("描述希望生成或编辑的图片内容", "一只橘猫坐在窗台上晒太阳，暖色调，写实风格"),
			{Name: "aspect_ratio", Type: "string", Label: "宽高比",
				Options: []string{"1:1", "16:9", "9:16", "4:3", "3:4"}, Default: "1:1"},
			{Name: "quality", Type: "string", Label: "渲染质量",
				Options: []string{"auto", "low", "medium", "high"}, Default: "auto"},
			{Name: "resolution", Type: "string", Label: "分辨率档位", Options: []string{"1K", "2K", "4K"}},
			referenceImagesParam("参考图片 URL 数组"),
		},
	},
}

// videoCapabilities 视频生成能力。
var videoCapabilities = []Capability{
	{
		Kind: KindVideoSeedance, Group: GroupVideo, Vendor: "即梦", Label: "即梦 Seedance 视频",
		Desc:       "支持文生视频、图生视频、首尾帧",
		SubmitPath: "/api/v2/video/generate", QueryPath: queryVideo, Price: 100, Outputs: 1,
		Params: []CapParam{
			promptParam("描述视频内容", "一只可爱的小猫在花园里玩耍，阳光明媚"),
			{Name: "model", Type: "string", Label: "模型", Description: "文生/图生与首尾帧用不同模型",
				Options: []string{"doubao-seedance-1-0-pro-fast-251015", "doubao-seedance-1-0-lite-i2v-250428"},
				Default: "doubao-seedance-1-0-pro-fast-251015"},
			{Name: "ratio", Type: "string", Label: "宽高比",
				Options: []string{"16:9", "9:16", "1:1", "4:3", "3:4", "adaptive"}, Default: "16:9"},
			{Name: "resolution", Type: "string", Label: "分辨率",
				Options: []string{"480p", "720p", "1080p"}, Default: "720p"},
			{Name: "duration", Type: "number", Label: "时长（秒）", Description: "2~12 秒", Default: 5},
			referenceImagesParam("图生视频 / 首尾帧的参考图 URL"),
		},
	},
	{
		Kind: KindVideoSeedance25, Group: GroupVideo, Vendor: "即梦", Label: "Seedance 2.5 视频",
		Desc:       "更新一代 Seedance 视频模型",
		SubmitPath: "/api/v2/video/seedance2-5", QueryPath: queryVideo, Price: 150, Outputs: 1,
		Params: []CapParam{
			promptParam("描述视频内容", "一只可爱的小猫在花园里玩耍，阳光明媚"),
			{Name: "ratio", Type: "string", Label: "宽高比",
				Options: []string{"16:9", "9:16", "1:1", "adaptive"}, Default: "16:9"},
			{Name: "resolution", Type: "string", Label: "分辨率",
				Options: []string{"480p", "720p", "1080p"}, Default: "720p"},
			{Name: "duration", Type: "number", Label: "时长（秒）", Default: 5},
			referenceImagesParam("参考图 URL"),
		},
	},
	{
		Kind: KindVideoVeo, Group: GroupVideo, Vendor: "Gemini", Label: "Veo 视频",
		Desc:       "Google Veo，支持参考图模式",
		SubmitPath: "/api/v2/veo/generate", QueryPath: queryVeo, Price: 200, Outputs: 1,
		Params: []CapParam{
			{Name: "model", Type: "string", Required: true, Label: "模型", Description: "Veo 模型名称",
				Options: []string{"veo-3.1", "veo-3.1-fast", "veo-3.0"}, Default: "veo-3.1"},
			promptParam("视频生成提示词", "一只可爱的小猫在花园里玩耍，阳光明媚"),
			{Name: "aspect_ratio", Type: "string", Label: "宽高比",
				Options: []string{"16:9", "9:16"}, Default: "16:9"},
			{Name: "resolution", Type: "string", Label: "分辨率", Options: []string{"720p", "1080p"}, Default: "720p"},
			{Name: "duration", Type: "string", Label: "时长（秒）", Description: "Veo 3.1 仅支持 8 秒", Default: "8"},
			{Name: "generation_type", Type: "string", Label: "参考图模式",
				Description: "frame 只能传 1 张图；omni-flash 支持 1 或 3 张",
				Options:     []string{"frame", "omni-flash"}},
			referenceImagesParam("参考图 URL"),
		},
	},
	{
		Kind: KindVideoGrok, Group: GroupVideo, Vendor: "Grok", Label: "Grok 视频",
		Desc:       "xAI Grok 视频生成",
		SubmitPath: "/api/v2/video/grok", QueryPath: queryGrok, Price: 150, Outputs: 1,
		Params: []CapParam{
			promptParam("视频生成提示词", "一只可爱的小猫在花园里玩耍，阳光明媚"),
			{Name: "ratio", Type: "string", Label: "宽高比", Options: []string{"16:9", "9:16", "1:1"}, Default: "16:9"},
			{Name: "duration", Type: "number", Label: "时长（秒）", Default: 6},
			referenceImagesParam("参考图 URL"),
		},
	},
	{
		Kind: KindVideoMiniMaxH3, Group: GroupVideo, Vendor: "MiniMax", Label: "MiniMax H3 视频",
		Desc:       "多模态视频生成，按秒计费，成功后多退少补",
		SubmitPath: "/api/v2/video/minimax-h3", QueryPath: queryVideo, Price: 250, Outputs: 1,
		Params: []CapParam{
			promptParam("视频生成提示词", "一只可爱的小猫在花园里玩耍，阳光明媚"),
			{Name: "resolution", Type: "string", Required: true, Label: "分辨率",
				Description: "仅支持 768P、2K，区分大小写",
				Options:     []string{"768P", "2K"}, Default: "768P"},
			{Name: "duration", Type: "number", Required: true, Label: "时长（秒）",
				Description: "4~15 秒整数", Default: 6},
			{Name: "ratio", Type: "string", Label: "宽高比",
				Description: "文生视频必填具体比例，不能为 adaptive",
				Options:     []string{"16:9", "9:16", "1:1", "adaptive"}, Default: "16:9"},
			{Name: "aigc_watermark", Type: "boolean", Label: "AIGC 水印", Default: false},
			referenceImagesParam("参考图 URL"),
		},
	},
}

// capabilityIndex 按任务类型索引全部能力。
var capabilityIndex = func() map[TaskKind]Capability {
	index := make(map[TaskKind]Capability)
	for _, list := range [][]Capability{imageCapabilities, videoCapabilities} {
		for _, c := range list {
			index[c.Kind] = c
		}
	}
	return index
}()

// CapabilityOf 返回某任务类型的能力定义。
func CapabilityOf(kind TaskKind) (Capability, bool) {
	c, ok := capabilityIndex[kind]
	return c, ok
}

// Capabilities 返回指定分组的能力列表，group 为空时返回全部。
func Capabilities(group string) []Capability {
	var out []Capability
	if group == "" || group == GroupImage {
		out = append(out, imageCapabilities...)
	}
	if group == "" || group == GroupVideo {
		out = append(out, videoCapabilities...)
	}
	for i := range out {
		out[i].Price = PriceOf(out[i].Kind)
	}
	return out
}

// GroupOf 返回任务类型所属分组，音乐类任务统一归入 music。
func GroupOf(kind TaskKind) string {
	if c, ok := capabilityIndex[kind]; ok {
		return c.Group
	}
	return GroupMusic
}

func init() {
	// 把能力定价并入统一价目表，便于结算与后台展示
	for kind, c := range capabilityIndex {
		Price[kind] = c.Price
		KindLabels[kind] = c.Label
	}
}
