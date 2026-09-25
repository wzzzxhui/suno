package mv

import (
	"testing"

	"github.com/lepro/suno-open-api/internal/model"
)

var testPricing = Pricing{
	VideoPerSecond: map[string]float64{"480p": 3.33, "720p": 7.74, "1080p": 16.65},
	ImageCost:      map[string]int64{"480p": 20, "720p": 20, "1080p": 25},
	Markup:         2,
	MinPrice:       100,
}

func TestQuote(t *testing.T) {
	// 与 2026-09 实测的 176.4 秒歌曲一致：15 个镜头，每段约 11.8 秒按 12 秒计费
	eco := testPricing.Quote(Plan(176.4, "", PlanOptions{Tier: model.MVTierEconomy}), "480p", false)
	if eco.Shots != 15 || eco.Images != 15 || eco.Cost != 300 || eco.Price != 600 {
		t.Fatalf("经济版报价不对（实测上游扣 300）: %+v", eco)
	}

	// 12 秒 480p 视频实测 40 积分
	pre := testPricing.Quote(Plan(176.4, "", PlanOptions{Tier: model.MVTierPremium}), "480p", false)
	if pre.Videos != 15 || pre.Cost != 15*40 {
		t.Fatalf("高级版 480p 成本不对: %+v", pre)
	}
	hd := testPricing.Quote(Plan(176.4, "", PlanOptions{Tier: model.MVTierPremium}), "1080p", false)
	if hd.Cost != 15*200 || hd.Price != 6000 {
		t.Fatalf("高级版 1080p 报价不对: %+v", hd)
	}

	// 复用镜头不计成本
	segs := Plan(96, lyrics, PlanOptions{Tier: model.MVTierPremium, ReuseChorus: true})
	q := testPricing.Quote(segs, "480p", false)
	if q.Reused == 0 || q.Videos+q.Reused != len(segs) || q.Cost != int64(q.Videos)*40 {
		t.Fatalf("复用镜头应免费: %+v", q)
	}

	// 有参考图时每段视频多一张首帧图
	kf := testPricing.Quote(Plan(176.4, "", PlanOptions{Tier: model.MVTierPremium}), "480p", true)
	if kf.Keyframes != 15 || kf.Cost != 15*(40+20) {
		t.Fatalf("首帧成本不对: %+v", kf)
	}
	img := testPricing.Quote(Plan(176.4, "", PlanOptions{Tier: model.MVTierEconomy}), "480p", true)
	if img.Keyframes != 0 || img.Cost != 300 {
		t.Fatalf("图片镜头不需要首帧: %+v", img)
	}

	if short := testPricing.Quote(Plan(5, "", PlanOptions{Tier: model.MVTierEconomy}), "480p", false); short.Price != 100 {
		t.Fatalf("应不低于最低价: %+v", short)
	}
}
