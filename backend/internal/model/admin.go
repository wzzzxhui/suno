package model

import (
	"net/url"
	"strconv"
	"time"
)

// AdminUser 运营后台账号。
type AdminUser struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	Nickname     string     `json:"nickname"`
	Role         string     `json:"role"`
	Status       int        `json:"status"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// MerchantRow 商户列表行，额外带上密钥数量与用量统计。
type MerchantRow struct {
	Merchant
	KeyCount      int64      `json:"key_count"`
	TaskCount     int64      `json:"task_count"`
	ConsumedTotal int64      `json:"consumed_total"`
	LastActiveAt  *time.Time `json:"last_active_at"`
}

// TaskRow 任务列表行，附带商户名称便于后台展示。
type TaskRow struct {
	Task
	MerchantName string `json:"merchant_name"`
	Kind         string `json:"kind"`
	KindLabel    string `json:"kind_label"`
}

// PointLogRow 积分流水行，附带商户名称。
type PointLogRow struct {
	PointLog
	MerchantName string `json:"merchant_name"`
}

// SongRow 作品库中的一首歌，附带封面、音频与已生成的 MV 信息。
type SongRow struct {
	TaskID       int64     `json:"task_id"`
	MerchantID   int64     `json:"merchant_id"`
	MerchantName string    `json:"merchant_name"`
	CustomID     string    `json:"custom_id"`
	Title        string    `json:"title"`
	Kind         string    `json:"kind"`
	KindLabel    string    `json:"kind_label"`
	FileInfo     *FileInfo `json:"fileInfo,omitempty"`
	ProxyURL     string    `json:"proxy_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`

	// 该作品最近一次 MV 生成任务
	VideoTaskID  *int64 `json:"video_task_id,omitempty"`
	VideoStatus  string `json:"video_status,omitempty"`
	VideoURL     string `json:"video_url,omitempty"`
	VideoMessage string `json:"video_message,omitempty"`

	// 已签发的创作证明编号，未签发为空
	CertificateNo string `json:"certificate_no,omitempty"`
}

// SongFilter 作品库查询条件。
type SongFilter struct {
	MerchantID int64
	Keyword    string
	Page       int
	Size       int
}

// Overview 仪表盘概览数据。
type Overview struct {
	MerchantTotal   int64 `json:"merchant_total"`
	MerchantActive  int64 `json:"merchant_active"`
	KeyTotal        int64 `json:"key_total"`
	PointsRemaining int64 `json:"points_remaining"`

	TaskTotal     int64 `json:"task_total"`
	TaskToday     int64 `json:"task_today"`
	TaskRunning   int64 `json:"task_running"`
	TaskFailed    int64 `json:"task_failed"`
	TaskCompleted int64 `json:"task_completed"`

	ConsumedTotal  int64 `json:"consumed_total"`
	ConsumedToday  int64 `json:"consumed_today"`
	RefundedTotal  int64 `json:"refunded_total"`
	RechargedTotal int64 `json:"recharged_total"`
}

// TrendPoint 单日趋势数据，用于折线图。
type TrendPoint struct {
	Date      string `json:"date"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Failed    int64  `json:"failed"`
	Consumed  int64  `json:"consumed"`
}

// KindStat 任务类型分布，用于饼图。
type KindStat struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Count    int64  `json:"count"`
	Consumed int64  `json:"consumed"`
}

// TaskFilter 任务列表查询条件。
type TaskFilter struct {
	MerchantID int64
	Kind       string
	Status     string
	Keyword    string
	Start      string
	End        string
	Page       int
	Size       int
}

// PointLogFilter 积分流水查询条件。
type PointLogFilter struct {
	MerchantID int64
	Type       int
	Start      string
	End        string
	Page       int
	Size       int
}

// KindLabels 任务类型的中文名，前后台共用。
var KindLabels = map[TaskKind]string{
	KindGenerate:      "生成音乐",
	KindSound:         "生成音效",
	KindUpload:        "上传参考音频",
	KindWholeSong:     "获取整首歌",
	KindAlignedLyrics: "获取歌词时间戳",
	KindUpsample:      "Remaster 音乐",
	KindVideo:         "生成音乐视频",
	KindCrop:          "裁剪音乐",
	KindSpeed:         "调整音乐速度",
	KindDownloadWAV:   "下载 WAV",
	KindDownloadMP3:   "下载 MP3",
	KindDownloadM4A:   "下载 M4A",
	KindVoiceTrain:    "训练音色",
	KindVoiceCover:    "音色翻唱",
	KindVoiceClone:    "创建演唱音色",
	KindVoiceSong:     "音色演唱创作",
}

// LabelOf 返回任务类型的中文名，未知类型回退为原始值。
func LabelOf(kind TaskKind) string {
	if label, ok := KindLabels[kind]; ok {
		return label
	}
	return string(kind)
}

// AllKinds 按后台展示顺序返回全部任务类型。
func AllKinds() []TaskKind {
	return []TaskKind{
		KindGenerate, KindSound, KindUpload, KindWholeSong, KindAlignedLyrics,
		KindUpsample, KindVideo, KindCrop, KindSpeed,
		KindDownloadWAV, KindDownloadMP3, KindDownloadM4A,
		KindVoiceTrain, KindVoiceCover, KindVoiceClone, KindVoiceSong,
	}
}

// Voice 音色库中的一个音色，由一次「训练音色」任务产生。
type Voice struct {
	TaskID       int64      `json:"task_id"`
	MerchantID   int64      `json:"merchant_id"`
	MerchantName string     `json:"merchant_name"`
	Name         string     `json:"name"`
	ModelName    string     `json:"model_name"`
	Status       TaskStatus `json:"status"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`

	// 训练中才有：上游进度，以及最近一次向上游确认状态的时间（持续刷新说明还在跟进）
	Progress  *TaskProgress `json:"progress,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

// ProxyAlive 判断上游签名代理地址是否仍在有效期内。
// 代理地址带 expires（秒级时间戳）参数，过期后请求会返回 403；没有该参数的地址视为长期有效。
func ProxyAlive(proxyURL string, now time.Time) bool {
	if proxyURL == "" {
		return false
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return false
	}
	raw := u.Query().Get("expires")
	if raw == "" {
		return true
	}
	expires, err := strconv.ParseInt(raw, 10, 64)
	// 留一分钟余量，避免刚取到就过期
	return err == nil && now.Unix() < expires-60
}
