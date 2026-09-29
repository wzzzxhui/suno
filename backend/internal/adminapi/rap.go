package adminapi

import (
	"regexp"
	"strings"

	"github.com/lepro/suno-open-api/internal/httpx"
)

var rapStylePattern = regexp.MustCompile(`(?i)说唱|嘻哈|饶舌|快嘴|\b(?:rap|rapping|trap|drill|grime|hip[\s-]?hop|boom[\s-]?bap|spoken[\s-]?word)\b`)
var rapSectionPattern = regexp.MustCompile(`(?im)^\s*\[(?:rap|rapping|trap|drill|grime|hip[\s-]?hop|spoken[\s-]?word|说唱|嘻哈|饶舌)[^\]]*\]`)

func validateRapExclusion(req *adminGenerateRequest) error {
	if !req.ExcludeRap {
		return nil
	}
	if rapStylePattern.MatchString(req.Tags) || rapStylePattern.MatchString(req.GPTDescriptionPrompt) || rapSectionPattern.MatchString(req.Prompt) {
		return httpx.BadRequest("已开启排除说唱，请移除风格描述或歌词段落中的说唱提示")
	}
	return nil
}

func rapExclusionTags(existing string) string {
	tags := []string{"rap", "hip hop", "trap", "drill", "grime", "boom bap", "spoken word", "rapping"}
	seen := make(map[string]bool)
	result := make([]string, 0, len(tags)+4)
	for _, part := range strings.Split(existing, ",") {
		tag := strings.TrimSpace(part)
		key := strings.ToLower(tag)
		if tag != "" && !seen[key] {
			seen[key] = true
			result = append(result, tag)
		}
	}
	for _, tag := range tags {
		if !seen[tag] {
			result = append(result, tag)
		}
	}
	return strings.Join(result, ", ")
}
