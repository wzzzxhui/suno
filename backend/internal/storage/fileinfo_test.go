package storage

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseFileInfoArchived(t *testing.T) {
	SetFileSigner(func(key string) string { return "https://cos.example/" + key + "?sig=1" })
	defer SetFileSigner(nil)

	raw := `{"mp3Url":"http://cn3.m4a.bmnmny.cn/a.m4a","coverUrl":"https://cdn2.suno.ai/a.jpg","duration":12,
		"archived":{"audio":"songs/m1/9.m4a","cover":"songs/m1/9_cover.jpeg"}}`
	info := parseFileInfo(sql.NullString{String: raw, Valid: true})
	if info.MP3URL != "https://cos.example/songs/m1/9.m4a?sig=1" || info.CoverURL != "https://cos.example/songs/m1/9_cover.jpeg?sig=1" {
		t.Fatalf("已转存的作品应返回平台副本地址: %+v", info)
	}
	if info.Archived == nil || info.Archived.Audio != "songs/m1/9.m4a" || info.Duration != 12 {
		t.Fatalf("应保留转存信息与其他字段: %+v", info)
	}
	// 转存路径不对外输出
	out, _ := json.Marshal(info)
	if strings.Contains(string(out), "archived") || strings.Contains(string(out), "bmnmny") {
		t.Fatalf("对外输出不应包含转存路径或失效的上游地址: %s", out)
	}

	plain := parseFileInfo(sql.NullString{String: `{"mp3Url":"http://x/a.mp3"}`, Valid: true})
	if plain.MP3URL != "http://x/a.mp3" || plain.Archived != nil {
		t.Fatalf("未转存的作品应保持原样: %+v", plain)
	}
}
