package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decode(t *testing.T, body string) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (%s)", err, body)
	}
	return env
}

func TestRouterDispatch(t *testing.T) {
	r := NewRouter()
	r.GET("/api/v1/points/balance", func(w http.ResponseWriter, _ *http.Request) error {
		JSON(w, map[string]interface{}{"remaining_points": 1000})
		return nil
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/points/balance", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	env := decode(t, rec.Body.String())
	if !env.Success || env.Code != 200 {
		t.Errorf("响应封装不符: %+v", env)
	}
}

func TestRouterTrailingSlash(t *testing.T) {
	r := NewRouter()
	r.GET("/healthz", func(w http.ResponseWriter, _ *http.Request) error {
		JSON(w, "ok")
		return nil
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("带尾斜杠也应命中路由，实际 %d", rec.Code)
	}
}

func TestRouterMethodNotAllowed(t *testing.T) {
	r := NewRouter()
	r.POST("/api/v1/music/generate", func(w http.ResponseWriter, _ *http.Request) error {
		JSON(w, nil)
		return nil
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/music/generate", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 405，实际 %d", rec.Code)
	}
}

func TestRouterNotFound(t *testing.T) {
	r := NewRouter()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d", rec.Code)
	}
}

func TestErrorEnvelope(t *testing.T) {
	r := NewRouter()
	r.GET("/fail", func(_ http.ResponseWriter, _ *http.Request) error {
		return NoPoints("积分不足，请先充值")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fail", nil))

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("期望 402，实际 %d", rec.Code)
	}
	env := decode(t, rec.Body.String())
	if env.Success || env.Code != 402 || env.Message != "积分不足，请先充值" {
		t.Errorf("错误响应不符: %+v", env)
	}
}

func TestMiddlewareOrder(t *testing.T) {
	var order []string

	tag := func(name string) Middleware {
		return func(next Handler) Handler {
			return func(w http.ResponseWriter, r *http.Request) error {
				order = append(order, name)
				return next(w, r)
			}
		}
	}

	r := NewRouter()
	r.Use(tag("global"))
	r.GET("/x", func(w http.ResponseWriter, _ *http.Request) error {
		order = append(order, "handler")
		JSON(w, nil)
		return nil
	}, tag("route"))

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	want := []string{"global", "route", "handler"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("中间件顺序应为 %v，实际 %v", want, order)
	}
}

func TestDecodeJSONAllowsUnknownFields(t *testing.T) {
	var dst struct {
		Title string `json:"title"`
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"夏日","future_field":1}`))
	if err := DecodeJSON(req, &dst); err != nil {
		t.Fatalf("多余字段不应报错: %v", err)
	}
	if dst.Title != "夏日" {
		t.Errorf("解析结果不符: %q", dst.Title)
	}
}

func TestDecodeJSONRejectsBadBody(t *testing.T) {
	var dst struct{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{oops}`))
	err := DecodeJSON(req, &dst)
	if err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != http.StatusBadRequest {
		t.Errorf("应返回 400，实际 %v", err)
	}
}

func TestQueryHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?page=3&id=199824&bad=abc", nil)

	if got := QueryInt(req, "page", 1); got != 3 {
		t.Errorf("page 应为 3，实际 %d", got)
	}
	if got := QueryInt(req, "missing", 10); got != 10 {
		t.Errorf("缺省值应为 10，实际 %d", got)
	}
	if got := QueryInt(req, "bad", 7); got != 7 {
		t.Errorf("非法值应回落到缺省 7，实际 %d", got)
	}

	id, err := QueryInt64(req, "id")
	if err != nil || id != 199824 {
		t.Errorf("id 解析失败: %v %d", err, id)
	}
	if _, err := QueryInt64(req, "missing"); err == nil {
		t.Error("缺少参数应返回错误")
	}
	if _, err := QueryInt64(req, "bad"); err == nil {
		t.Error("非数字参数应返回错误")
	}
}
