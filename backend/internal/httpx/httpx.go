// Package httpx 提供统一的响应封装、错误码与一个适配 Go 1.18 的轻量路由。
package httpx

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Envelope 是所有接口的统一响应结构。
type Envelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Success bool        `json:"success"`
}

// APIError 带业务状态码的错误。
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string { return e.Message }

// 预定义错误，对应文档中的错误码表。
func BadRequest(msg string) *APIError { return &APIError{Code: http.StatusBadRequest, Message: msg} }
func Unauthorized(msg string) *APIError {
	return &APIError{Code: http.StatusUnauthorized, Message: msg}
}
func NoPoints(msg string) *APIError { return &APIError{Code: http.StatusPaymentRequired, Message: msg} }
func NotFound(msg string) *APIError { return &APIError{Code: http.StatusNotFound, Message: msg} }
func TooManyRequests(msg string) *APIError {
	return &APIError{Code: http.StatusTooManyRequests, Message: msg}
}
func Internal(msg string) *APIError {
	return &APIError{Code: http.StatusInternalServerError, Message: msg}
}

// JSON 输出成功响应。
func JSON(w http.ResponseWriter, data interface{}) {
	write(w, http.StatusOK, Envelope{Code: http.StatusOK, Message: "请求成功", Data: data, Success: true})
}

// Fail 输出错误响应，HTTP 状态码与业务 code 保持一致。
func Fail(w http.ResponseWriter, err error) {
	apiErr, ok := err.(*APIError)
	if !ok {
		log.Printf("[error] %v", err)
		apiErr = Internal("服务内部错误，请稍后重试")
	}
	write(w, apiErr.Code, Envelope{Code: apiErr.Code, Message: apiErr.Message, Data: nil, Success: false})
}

func write(w http.ResponseWriter, status int, body Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("[error] 写出响应失败: %v", err)
	}
}

// DecodeJSON 解析请求体，空体视为空对象。
func DecodeJSON(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return nil
	}
	// 允许多余字段：调用方按文档多传参数时不应直接 400
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		if err.Error() == "EOF" {
			return nil
		}
		return BadRequest("请求体不是合法 JSON：" + err.Error())
	}
	return nil
}

// QueryInt 读取整型 query 参数。
func QueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

// QueryInt64 读取 int64 query 参数，缺失或非法时返回错误。
func QueryInt64(r *http.Request, key string) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, BadRequest("缺少参数 " + key)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, BadRequest("参数 " + key + " 必须是数字")
	}
	return n, nil
}

// Handler 是带错误返回的处理函数，便于集中处理错误响应。
type Handler func(http.ResponseWriter, *http.Request) error

// Middleware 装饰 Handler。
type Middleware func(Handler) Handler

// Router 是一个只做「方法 + 精确路径」匹配的轻量路由，
// 避免依赖 Go 1.22 才有的 ServeMux 方法模式。
type Router struct {
	routes      map[string]Handler
	middlewares []Middleware
	notFound    Handler
}

// NewRouter 创建路由。
func NewRouter() *Router {
	return &Router{
		routes: make(map[string]Handler),
		notFound: func(w http.ResponseWriter, r *http.Request) error {
			return NotFound("接口不存在：" + r.URL.Path)
		},
	}
}

// Use 追加全局中间件，按注册顺序由外到内执行。
func (rt *Router) Use(mw ...Middleware) {
	rt.middlewares = append(rt.middlewares, mw...)
}

// Handle 注册路由。
func (rt *Router) Handle(method, path string, h Handler, mw ...Middleware) {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	rt.routes[strings.ToUpper(method)+" "+path] = h
}

// GET 注册 GET 路由。
func (rt *Router) GET(path string, h Handler, mw ...Middleware) {
	rt.Handle(http.MethodGet, path, h, mw...)
}

// POST 注册 POST 路由。
func (rt *Router) POST(path string, h Handler, mw ...Middleware) {
	rt.Handle(http.MethodPost, path, h, mw...)
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimRight(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}

	handler, ok := rt.routes[r.Method+" "+path]
	if !ok {
		// 路径存在但方法不匹配时，回 405 更利于排查
		for key := range rt.routes {
			if strings.HasSuffix(key, " "+path) {
				handler = func(w http.ResponseWriter, r *http.Request) error {
					return &APIError{Code: http.StatusMethodNotAllowed, Message: "请求方法不被支持"}
				}
				break
			}
		}
		if handler == nil {
			handler = rt.notFound
		}
	}

	for i := len(rt.middlewares) - 1; i >= 0; i-- {
		handler = rt.middlewares[i](handler)
	}

	if err := handler(w, r); err != nil {
		Fail(w, err)
	}
}
