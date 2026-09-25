package model

import "time"

// MV 生成方式
const (
	MVModeAuto   = "auto"   // AI 写分镜后直接生成
	MVModeManual = "manual" // AI 先写草稿，运营逐段修改后再生成
)

// MV 档位：决定每个镜头用图片动效还是 AI 视频
const (
	MVTierEconomy  = "economy"  // 全部图片动效
	MVTierStandard = "standard" // 副歌与开场用 AI 视频，其余图片动效
	MVTierPremium  = "premium"  // 全部 AI 视频
)

// 镜头素材类型
const (
	ShotVideo = "video"
	ShotImage = "image"
	ShotReuse = "reuse" // 复用另一个镜头的素材，不再生成
)

// MV 项目状态
const (
	MVDraft      = "draft"
	MVGenerating = "generating"
	MVComposing  = "composing"
	MVCompleted  = "completed"
	MVFailed     = "failed"
)

// 分镜片段状态
const (
	SegPending   = "pending"
	SegRunning   = "running"
	SegCompleted = "completed"
	SegFailed    = "failed"
)

// MV 画风预设
const (
	MVStyleRealistic    = "realistic"    // 真实摄影
	MVStyleCinematic    = "cinematic"    // 电影剧照
	MVStyleAnime        = "anime"        // 日系动画
	MVStyleGuofeng      = "guofeng"      // 国风
	MVStyle3D           = "3d"           // 3D 动画
	MVStyleIllustration = "illustration" // 手绘插画
)

// MVMaxRefs 参考图最多张数：太多会稀释人物特征，上游也有数量限制
const MVMaxRefs = 3

// MVLook 全片统一的形象设定：画风、人物、服装、场景、色调与参考图。
type MVLook struct {
	Style     string `json:"style"`
	Character string `json:"character"` // 主角外貌：性别、年龄、长相、发型等
	Outfit    string `json:"outfit"`
	Scene     string `json:"scene"`
	Palette   string `json:"palette"`
	// RefKeys 人物 / 服装参考图在 COS 中的路径（运营上传或生成的定妆照）
	RefKeys []string `json:"ref_keys"`
	// RefURLs 查询时按 RefKeys 签出的临时地址，仅供展示
	RefURLs []string `json:"ref_urls,omitempty"`
}

// HasRefs 是否设置了参考图。
func (l MVLook) HasRefs() bool { return len(l.RefKeys) > 0 }

// MVProject 一条长 MV：由若干分镜片段拼接而成，配原曲音轨。
type MVProject struct {
	ID             int64      `json:"id"`
	MerchantID     int64      `json:"merchant_id"`
	MerchantName   string     `json:"merchant_name,omitempty"`
	SongTaskID     int64      `json:"song_task_id"`
	Title          string     `json:"title"`
	Mode           string     `json:"mode"`
	Tier           string     `json:"tier"`
	ReuseChorus    bool       `json:"reuse_chorus"`
	Subtitles      bool       `json:"subtitles"`
	Status         string     `json:"status"`
	StyleNote      string     `json:"style_note"`
	VisualBible    string     `json:"visual_bible"`
	Writer         string     `json:"writer"`
	Ratio          string     `json:"ratio"`
	Resolution     string     `json:"resolution"`
	Look           MVLook     `json:"look"`
	UseCover       bool       `json:"use_cover"`
	CoverURL       string     `json:"cover_url"`
	AudioURL       string     `json:"-"`
	Duration       float64    `json:"duration"`
	PointsCost     int64      `json:"points_cost"`
	PointsRefunded bool       `json:"points_refunded"`
	VideoKey       string     `json:"-"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`

	// 以下为查询时补充的展示字段
	SegmentTotal int          `json:"segment_total"`
	SegmentDone  int          `json:"segment_done"`
	Quote        int64        `json:"quote"` // 建草稿时定下的固定价
	VideoURL     string       `json:"video_url,omitempty"`
	Segments     []*MVSegment `json:"segments,omitempty"`
}

// MVSegment 一个分镜片段。
type MVSegment struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"project_id"`
	Seq       int     `json:"seq"`
	Kind      string  `json:"kind"`
	ReuseOf   *int    `json:"reuse_of,omitempty"`
	Section   string  `json:"section"`
	Start     float64 `json:"start_s"`
	End       float64 `json:"end_s"`
	Lyrics    string  `json:"lyrics"`
	Prompt    string  `json:"prompt"`
	// KeyframeURL 视频镜头先按参考图画出的首帧，再以它为起点生成视频，保证人物一致
	KeyframeURL    string `json:"keyframe_url,omitempty"`
	Status         string `json:"status"`
	ProviderTaskID string `json:"-"`
	// VideoURL 镜头素材地址：视频镜头为 mp4，图片镜头为图片
	VideoURL     string    `json:"video_url,omitempty"`
	Attempts     int       `json:"attempts"`
	ErrorMessage string    `json:"error_message,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Length 返回片段时长（秒）。
func (s *MVSegment) Length() float64 { return s.End - s.Start }
