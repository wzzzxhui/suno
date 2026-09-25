package model

import (
	"testing"
	"time"
)

func TestProxyAlive(t *testing.T) {
	now := time.Unix(1790162500, 0)
	base := "https://open.mxapi.org/api/v2/proxy/resource?url=x&token=t&expires="

	cases := []struct {
		url  string
		want bool
	}{
		{base + "1790170000", true},   // 两小时后才过期
		{base + "1790162530", false},  // 不足一分钟余量
		{base + "1790100000", false},  // 已过期
		{"https://cdn.x/a.m4a", true}, // 不带 expires 视为长期有效
		{"", false},
		{base + "abc", false},
	}
	for _, c := range cases {
		if got := ProxyAlive(c.url, now); got != c.want {
			t.Errorf("ProxyAlive(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}
