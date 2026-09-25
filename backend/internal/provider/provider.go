// Package provider 抽象音乐生成的上游能力：本地 mock 与真实 Suno 接口两种实现。
package provider

import (
	"context"

	"github.com/lepro/suno-open-api/internal/model"
)

// SubmitRequest 提交给上游的一次任务请求。
type SubmitRequest struct {
	Kind    model.TaskKind
	Payload map[string]interface{}
}

// SubmitResult 上游返回的任务标识。生成音乐、Remaster 会返回两个。
type SubmitResult struct {
	ProviderIDs []string
}

// FetchRequest 查询一个上游任务。
type FetchRequest struct {
	Kind       model.TaskKind
	ProviderID string
}

// FetchResult 上游任务的当前状态与产出。
type FetchResult struct {
	Status   model.TaskStatus
	CustomID string
	ProxyURL string
	FileInfo *model.FileInfo
	// Extend 是上游完整数据的 JSON 字符串，原样透传给调用方。
	Extend string
	Reason string
}

// Provider 是所有上游实现需要满足的接口。
type Provider interface {
	Name() string
	Submit(ctx context.Context, req *SubmitRequest) (*SubmitResult, error)
	Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error)
}

// OutputCount 返回某类任务会产出几个结果，用于建多少条任务记录。
func OutputCount(kind model.TaskKind) int {
	switch kind {
	case model.KindGenerate, model.KindUpsample:
		return 2
	default:
		return 1
	}
}
