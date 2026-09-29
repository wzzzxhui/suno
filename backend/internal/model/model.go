package model

import "time"

// TaskKind 任务类型，与对外接口一一对应。
type TaskKind string

const (
	KindGenerate      TaskKind = "generate"       // 生成音乐（含延长、翻唱）
	KindSound         TaskKind = "sound"          // 生成音效
	KindUpload        TaskKind = "upload"         // 上传参考音频
	KindWholeSong     TaskKind = "whole_song"     // 合成整首歌
	KindAlignedLyrics TaskKind = "aligned_lyrics" // 歌词时间戳
	KindUpsample      TaskKind = "upsample"       // Remaster
	KindVideo         TaskKind = "video"          // 生成音乐视频
	KindCrop          TaskKind = "crop"           // 裁剪
	KindSpeed         TaskKind = "speed"          // 变速
	KindDownloadWAV   TaskKind = "download_wav"
	KindDownloadMP3   TaskKind = "download_mp3"
	KindDownloadM4A   TaskKind = "download_m4a"

	// 音色类任务走腾讯多媒体实验室「唱歌克隆」，不经过 Suno 上游
	KindVoiceTrain TaskKind = "voice_train" // 训练音色模型
	KindVoiceCover TaskKind = "voice_cover" // 用音色模型翻唱

	// 演唱音色走 Mureka：上传清唱得到演唱音色，创作时直接用它演唱，不经过先生成再翻唱
	KindVoiceClone TaskKind = "voice_clone" // 创建演唱音色（同步完成）
	KindVoiceSong  TaskKind = "voice_song"  // 高级模式音乐创作（沿用已有任务类型）
)

// TaskStatus 任务状态。
type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusProcessing TaskStatus = "processing"
	StatusCompleted  TaskStatus = "completed"
	StatusFailed     TaskStatus = "failed"
)

// Terminal 表示任务是否已进入终态。
func (s TaskStatus) Terminal() bool {
	return s == StatusCompleted || s == StatusFailed
}

// Code 返回兼容旧版的数字状态码：1=排队 2=进行中 3=完成 4=失败。
func (s TaskStatus) Code() int {
	switch s {
	case StatusProcessing:
		return 2
	case StatusCompleted:
		return 3
	case StatusFailed:
		return 4
	default:
		return 1
	}
}

// 积分流水类型。
const (
	PointConsume  = 1 // 消耗
	PointRecharge = 2 // 充值
	PointAdjust   = 3 // 手动调整
	PointRefund   = 4 // 退还
)

// Merchant 商户。
type Merchant struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Status    int       `json:"status"` // 1=正常 0=禁用
	Points    int64     `json:"points"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// APIKey 商户的访问密钥。数据库只存哈希，明文仅在创建时返回一次。
type APIKey struct {
	ID         int64      `json:"id"`
	MerchantID int64      `json:"merchant_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Hash       string     `json:"-"`
	Status     int        `json:"status"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// FileInfo 任务产出的文件地址。
type FileInfo struct {
	MP3URL   string  `json:"mp3Url,omitempty"`
	MP4URL   string  `json:"mp4Url,omitempty"`
	WAVURL   string  `json:"wavUrl,omitempty"`
	M4AURL   string  `json:"m4aUrl,omitempty"`
	CoverURL string  `json:"coverUrl,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	// Images 图片类任务产出的图片地址
	Images []string `json:"images,omitempty"`
	// Archived 转存到平台 COS 的副本；存于 file_info.archived，不对外输出，读取时换成签名地址
	Archived *ArchivedFiles `json:"-"`
}

// ArchivedFiles 作品在平台 COS 中的副本路径。
type ArchivedFiles struct {
	Audio   string `json:"audio,omitempty"`
	Cover   string `json:"cover,omitempty"`
	MP3     string `json:"mp3,omitempty"`     // 供下载的 MP3 副本，源文件本就是 MP3 时与 Audio 相同
	Missing bool   `json:"missing,omitempty"` // 转存时上游已无可用音频，不再重试
}

// Task 一次异步任务。
type Task struct {
	ID             int64      `json:"id"`
	MerchantID     int64      `json:"-"`
	Kind           TaskKind   `json:"-"`
	Status         TaskStatus `json:"status"`
	StatusCode     int        `json:"status_code"`
	ProviderTaskID string     `json:"-"`
	CustomID       *string    `json:"custom_id"`
	ProxyURL       string     `json:"proxy_url,omitempty"`
	FileInfo       *FileInfo  `json:"fileInfo,omitempty"`
	Extend         string     `json:"extend,omitempty"`
	ExtraParam     string     `json:"extraParam,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	PointsCost     int64      `json:"points_cost"`
	PointsRefunded bool       `json:"points_refunded"`
	RetryCount     int        `json:"retry_count"`
	RetriedAt      *time.Time `json:"retried_at,omitempty"`
	Request        string     `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// TaskProgress 长任务（如音色训练）进行中时从上游取到的进度，存在任务的 extend 里。
// 上游不给百分比，只能展示所处阶段与已进行时长。
type TaskProgress struct {
	Stage      string     `json:"stage"`                 // queued=排队中 running=进行中
	StartedAt  *time.Time `json:"started_at,omitempty"`  // 上游开始执行的时间，排队时为空
	TotalEpoch int        `json:"total_epoch,omitempty"` // 音色训练的总轮次
}

// PointLog 积分流水。
type PointLog struct {
	ID         int64     `json:"id"`
	MerchantID int64     `json:"-"`
	Type       int       `json:"type"`
	Points     int64     `json:"points"`
	Balance    int64     `json:"balance"`
	Remark     string    `json:"remark"`
	TaskID     *int64    `json:"task_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Price 保留旧版定价数据；商户实际费用由 PriceOf 统一返回零。
var Price = map[TaskKind]int64{
	KindGenerate:      36,
	KindSound:         10,
	KindUpload:        1,
	KindWholeSong:     1,
	KindAlignedLyrics: 1,
	KindUpsample:      36,
	KindVideo:         1,
	KindCrop:          1,
	KindSpeed:         1,
	KindDownloadWAV:   5,
	KindDownloadMP3:   5,
	KindDownloadM4A:   5,
	// 腾讯侧按量计费，上线前按实际成本核定
	KindVoiceTrain: 100,
	KindVoiceCover: 20,
	// Mureka 按首计费，启动时可用 MUREKA_* 配置覆盖
	KindVoiceClone: 20,
	KindVoiceSong:  36,
}

// CopyrightAudioSurcharge 使用受版权保护音频时的商户附加费。
const CopyrightAudioSurcharge int64 = 0

// PriceOf 返回任务类型对商户的收费；平台服务免费。
func PriceOf(kind TaskKind) int64 {
	return 0 // 平台服务对商户免费；上游成本由平台账号承担。
}
