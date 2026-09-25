package task

import (
	"testing"

	"github.com/lepro/suno-open-api/internal/model"
)

func TestSplitCost(t *testing.T) {
	cases := []struct {
		name  string
		total int64
		n     int
		want  []int64
	}{
		{"生成音乐两首均摊", 36, 2, []int64{18, 18}},
		{"单任务全额", 5, 1, []int64{5}},
		{"余数给第一条", 7, 2, []int64{4, 3}},
		{"免费任务", 0, 2, []int64{0, 0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitCost(tc.total, tc.n)
			if len(got) != len(tc.want) {
				t.Fatalf("长度不符: got %v, want %v", got, tc.want)
			}
			var sum int64
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("第 %d 项: got %d, want %d", i, got[i], tc.want[i])
				}
				sum += got[i]
			}
			if sum != tc.total {
				t.Errorf("拆分后总额 %d 与原值 %d 不符", sum, tc.total)
			}
		})
	}
}

func TestSplitCostEmpty(t *testing.T) {
	if got := splitCost(10, 0); got != nil {
		t.Errorf("任务数为 0 时应返回 nil，实际 %v", got)
	}
}

func TestPriceCoverage(t *testing.T) {
	kinds := []model.TaskKind{
		model.KindGenerate, model.KindSound, model.KindUpload, model.KindWholeSong,
		model.KindAlignedLyrics, model.KindUpsample, model.KindVideo, model.KindCrop,
		model.KindSpeed, model.KindDownloadWAV, model.KindDownloadMP3, model.KindDownloadM4A,
	}
	for _, kind := range kinds {
		if model.PriceOf(kind) <= 0 {
			t.Errorf("任务类型 %s 缺少定价", kind)
		}
		if remarkOf(kind) == string(kind) {
			t.Errorf("任务类型 %s 缺少中文备注", kind)
		}
	}
}

func TestStatusCode(t *testing.T) {
	cases := map[model.TaskStatus]int{
		model.StatusPending:    1,
		model.StatusProcessing: 2,
		model.StatusCompleted:  3,
		model.StatusFailed:     4,
	}
	for status, want := range cases {
		if got := status.Code(); got != want {
			t.Errorf("%s 的数字状态码应为 %d，实际 %d", status, want, got)
		}
	}
	if !model.StatusCompleted.Terminal() || !model.StatusFailed.Terminal() {
		t.Error("completed 与 failed 应为终态")
	}
	if model.StatusPending.Terminal() || model.StatusProcessing.Terminal() {
		t.Error("pending 与 processing 不应为终态")
	}
}
