package provider

import (
	"context"

	"github.com/lepro/suno-open-api/internal/model"
)

// Router 按任务类型把请求分发到不同上游，未登记的类型走默认上游。
type Router struct {
	fallback Provider
	routes   map[model.TaskKind]Provider
}

// NewRouter 创建分发器。
func NewRouter(fallback Provider) *Router {
	return &Router{fallback: fallback, routes: make(map[model.TaskKind]Provider)}
}

// Route 把一组任务类型交给指定上游。
func (r *Router) Route(p Provider, kinds ...model.TaskKind) *Router {
	for _, k := range kinds {
		r.routes[k] = p
	}
	return r
}

func (r *Router) pick(kind model.TaskKind) Provider {
	if p, ok := r.routes[kind]; ok {
		return p
	}
	return r.fallback
}

// Name 实现 Provider，返回默认上游的名称。
func (r *Router) Name() string { return r.fallback.Name() }

// Submit 实现 Provider。
func (r *Router) Submit(ctx context.Context, req *SubmitRequest) (*SubmitResult, error) {
	return r.pick(req.Kind).Submit(ctx, req)
}

// Fetch 实现 Provider。
func (r *Router) Fetch(ctx context.Context, req *FetchRequest) (*FetchResult, error) {
	return r.pick(req.Kind).Fetch(ctx, req)
}
