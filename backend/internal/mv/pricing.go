package mv

import (
	"math"

	"github.com/lepro/suno-open-api/internal/model"
)

// Pricing 按上游实际成本给 MV 报价：逐个镜头累加成本，再乘加价倍数。
// 成本单位与平台积分相同（1 积分 = 0.01 元）。
type Pricing struct {
	// VideoPerSecond 各分辨率 Seedance 每秒成本；上游按单段向上取整计费
	VideoPerSecond map[string]float64
	// ImageCost 各分辨率单张图片成本（1080p 出 2K 图，其余出 1K 图）
	ImageCost map[string]int64
	Markup    float64 // 加价倍数，需覆盖失败重试与分镜撰写的成本
	MinPrice  int64
}

// Quote 一次报价的明细。
type Quote struct {
	Price  int64 `json:"price"`
	Cost   int64 `json:"cost"`
	Videos int   `json:"videos"`
	Images int   `json:"images"`
	Reused int   `json:"reused"`
	// Keyframes 设置了参考图时，视频镜头先按参考图画首帧，每段多一张图的成本
	Keyframes int `json:"keyframes"`
	Shots     int `json:"shots"`
}

// Quote 计算报价：售价 = 成本 × 加价倍数，按 10 积分向上取整，且不低于最低价。
// keyframes 为 true 时每个视频镜头额外计一张首帧图。
func (p Pricing) Quote(segs []*model.MVSegment, resolution string, keyframes bool) Quote {
	q := Quote{Shots: len(segs)}
	for _, s := range segs {
		switch s.Kind {
		case model.ShotVideo:
			q.Videos++
			q.Cost += int64(math.Ceil(p.VideoPerSecond[resolution] * float64(ClipSeconds(s))))
			if keyframes {
				q.Keyframes++
				q.Cost += p.ImageCost[resolution]
			}
		case model.ShotImage:
			q.Images++
			q.Cost += p.ImageCost[resolution]
		case model.ShotReuse:
			q.Reused++
		}
	}
	markup := p.Markup
	if markup <= 0 {
		markup = 2
	}
	q.Price = int64(math.Ceil(float64(q.Cost)*markup/10)) * 10
	if q.Price < p.MinPrice {
		q.Price = p.MinPrice
	}
	q.Price = 0 // 商户免费；Cost 保留平台上游成本估算。
	return q
}
