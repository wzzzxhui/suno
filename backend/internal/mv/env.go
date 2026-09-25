package mv

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// 常见系统自带或包管理器安装的中文字体位置（Windows 与主流 Linux 发行版）
var fontCandidates = []string{
	// Windows
	`C:\Windows\Fonts\msyh.ttc`,
	`C:\Windows\Fonts\msyh.ttf`,
	`C:\Windows\Fonts\simhei.ttf`,
	// Debian / Ubuntu：apt install fonts-noto-cjk 或 fonts-wqy-microhei
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
	// CentOS / Rocky / Fedora：dnf install google-noto-sans-cjk-ttc-fonts 或 wqy-microhei-fonts
	"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/google-noto-sans-cjk-fonts/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/wqy-microhei/wqy-microhei.ttc",
	// Alpine：apk add font-noto-cjk
	"/usr/share/fonts/noto/NotoSansCJK-Regular.ttc",
}

// ResolveFont 返回可用的字幕字体：优先使用配置的路径，不存在时在常见位置查找；都没有返回空串。
// 这样同一份配置在 Windows 与 Linux 上都能用。
func ResolveFont(configured string) string {
	for _, f := range append([]string{configured}, fontCandidates...) {
		if f == "" {
			continue
		}
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			return f
		}
	}
	return ""
}

// CheckFFmpeg 检查 ffmpeg 是否可用，以及合成用到的编码器与滤镜是否齐全，返回缺失项说明。
// 不同来源的 Linux 版 ffmpeg 编译选项不同，缺 libx264 或 drawtext 时成片会失败。
func CheckFFmpeg(ffmpeg string) []string {
	if _, err := exec.LookPath(ffmpeg); err != nil {
		return []string{"未找到 ffmpeg（" + ffmpeg + "）"}
	}
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, ffmpeg, append([]string{"-hide_banner"}, args...)...).Output()
		return string(out)
	}

	var missing []string
	if !strings.Contains(run("-encoders"), "libx264") {
		missing = append(missing, "缺少 libx264 编码器，无法输出 MP4")
	}
	filters := run("-filters")
	for _, f := range []struct{ name, use string }{{"zoompan", "图片运镜"}, {"drawtext", "歌词字幕"}} {
		if !strings.Contains(filters, " "+f.name+" ") {
			missing = append(missing, "缺少 "+f.name+" 滤镜（"+f.use+"）")
		}
	}
	return missing
}
