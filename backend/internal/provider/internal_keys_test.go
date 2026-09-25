package provider

import "testing"

func TestWithoutInternalKeys(t *testing.T) {
	in := map[string]interface{}{"title": "歌", "voice": map[string]interface{}{"model_name": "m1_x"}}
	out := withoutInternalKeys(in)
	if _, ok := out["voice"]; ok {
		t.Error("voice 不应发给上游")
	}
	if out["title"] != "歌" {
		t.Error("其他字段应保留")
	}
	if _, ok := in["voice"]; !ok {
		t.Error("不应修改原请求（重试还要用）")
	}

	plain := map[string]interface{}{"title": "歌"}
	if got := withoutInternalKeys(plain); len(got) != 1 {
		t.Errorf("没有内部字段时应原样返回：%v", got)
	}
}
