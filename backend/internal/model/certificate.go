package model

import "time"

// Certificate 作品的创作证明。同一作品只签发一份，首次签发扣费，之后重复下载免费。
// 证书内容在签发时固化，PDF 每次下载按这份记录重新绘制，内容保持一致。
type Certificate struct {
	ID            int64     `json:"-"`
	No            string    `json:"certificate_no"`
	MerchantID    int64     `json:"-"`
	TaskID        int64     `json:"task_id"`
	SunoID        string    `json:"suno_id"`
	Title         string    `json:"title"`
	Author        string    `json:"author"`
	Lyrics        string    `json:"-"`
	Tags          string    `json:"tags,omitempty"`
	ModelName     string    `json:"model,omitempty"`
	Duration      float64   `json:"duration"`
	AudioSHA256   string    `json:"audio_sha256"`
	AudioSize     int64     `json:"audio_size"`
	SongCreatedAt time.Time `json:"song_created_at"`
	PointsCost    int64     `json:"points_cost"`
	IssuedAt      time.Time `json:"issued_at"`
}
