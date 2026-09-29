package adminapi

import (
	"strings"
	"testing"

	"github.com/lepro/suno-open-api/internal/musicreq"
)

func TestRapExclusionTags(t *testing.T) {
	got := rapExclusionTags("jazz, RAP, trap, jazz")
	parts := strings.Split(got, ", ")
	seen := map[string]bool{}
	for _, part := range parts {
		key := strings.ToLower(part)
		if seen[key] {
			t.Fatalf("duplicate exclusion %q in %q", part, got)
		}
		seen[key] = true
	}
	for _, key := range []string{"jazz", "rap", "hip hop", "trap", "drill", "grime", "boom bap", "spoken word", "rapping"} {
		if !seen[key] {
			t.Fatalf("missing exclusion %q in %q", key, got)
		}
	}
}

func TestValidateRapExclusion(t *testing.T) {
	cases := []struct {
		name      string
		req       adminGenerateRequest
		wantError bool
	}{
		{"disabled permits rap", adminGenerateRequest{ExcludeRap: false, Generate: musicreq.Generate{Tags: "rap"}}, false},
		{"ordinary style", adminGenerateRequest{ExcludeRap: true, Generate: musicreq.Generate{Tags: "pop, jazz", Prompt: "[Verse]\nHello"}}, false},
		{"rap tag", adminGenerateRequest{ExcludeRap: true, Generate: musicreq.Generate{Tags: "hip-hop"}}, true},
		{"trap description", adminGenerateRequest{ExcludeRap: true, Generate: musicreq.Generate{GPTDescriptionPrompt: "一首 Trap 风格的歌曲"}}, true},
		{"rap section", adminGenerateRequest{ExcludeRap: true, Generate: musicreq.Generate{Prompt: "[Rap Verse]\nHello"}}, true},
		{"lyric mentions rap", adminGenerateRequest{ExcludeRap: true, Generate: musicreq.Generate{Prompt: "[Verse]\nI listen to rap"}}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateRapExclusion(&tt.req); (got != nil) != tt.wantError {
				t.Fatalf("validateRapExclusion() error = %v, wantError = %v", got, tt.wantError)
			}
		})
	}
}
