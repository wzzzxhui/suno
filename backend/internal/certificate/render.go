package certificate

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
)

// 证书按 A4 竖版、200 DPI 绘制成整页图片后放进 PDF。
const (
	pageW  = 1654
	pageH  = 2339
	margin = 170.0
)

var (
	colorPaper     = color.RGBA{0xFF, 0xFD, 0xF7, 0xFF}
	colorGold      = color.RGBA{0xAE, 0x8A, 0x3C, 0xFF}
	colorGoldLight = color.RGBA{0xE6, 0xD8, 0xB0, 0xFF}
	colorInk       = color.RGBA{0x1F, 0x29, 0x37, 0xFF}
	colorMuted     = color.RGBA{0x6B, 0x72, 0x80, 0xFF}
	colorRule      = color.RGBA{0xE8, 0xE2, 0xD3, 0xFF}
	colorSeal      = color.NRGBA{0xC8, 0x10, 0x2E, 0xB8}
)

// PDF 把证明绘制成 PDF。内容只取决于证明记录，重复下载得到同样的文件。
func (s *Service) PDF(c *model.Certificate) ([]byte, error) {
	f, err := s.fonts.load()
	if err != nil {
		return nil, httpx.Internal("创作证明字体不可用：" + err.Error() +
			"。请在 .env 中把 CERT_FONT_FILE（或 MV_FONT_FILE）指向含中文的字体文件")
	}

	img := render(f, c, s.opts.Issuer, s.VerifyLink(c.No), s.opts.VerifyURL)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buildPDF(buf.Bytes(), pageW, pageH, pdfInfo{
		Title:    "音乐作品创作证明 " + c.No,
		Author:   s.opts.Issuer,
		Subject:  fmt.Sprintf("《%s》 %s", c.Title, c.Author),
		Keywords: "SHA-256 " + c.AudioSHA256,
		Created:  c.IssuedAt,
	}), nil
}

/* ---------------------------------- 字体 ---------------------------------- */

// 未配置字体时依次尝试常见系统的中文字体。
var fallbackFonts = []string{
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
	`C:\Windows\Fonts\msyh.ttc`,
	`C:\Windows\Fonts\simhei.ttf`,
	"/System/Library/Fonts/PingFang.ttc",
}

type fontCache struct {
	path string
	once sync.Once
	font *sfnt.Font
	err  error
}

func (fc *fontCache) load() (*sfnt.Font, error) {
	fc.once.Do(func() {
		paths := fallbackFonts
		if fc.path != "" {
			paths = []string{fc.path}
		}
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				if fc.path != "" {
					fc.err = fmt.Errorf("读取字体 %s 失败", p)
				}
				continue
			}
			if fc.font, fc.err = pickFont(data); fc.err == nil {
				return
			}
		}
		if fc.err == nil {
			fc.err = errors.New("未找到中文字体")
		}
	})
	return fc.font, fc.err
}

// pickFont 从字体（或字体集合）里挑一个含中文的字形，集合中优先简体中文（如 Noto Sans CJK SC）。
func pickFont(data []byte) (*sfnt.Font, error) {
	coll, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, fmt.Errorf("字体文件无法解析：%v", err)
	}
	var buf sfnt.Buffer
	var first *sfnt.Font
	for i := 0; i < coll.NumFonts(); i++ {
		f, err := coll.Font(i)
		if err != nil {
			continue
		}
		if gi, err := f.GlyphIndex(&buf, '证'); err != nil || gi == 0 {
			continue
		}
		name, _ := f.Name(&buf, sfnt.NameIDFamily)
		if strings.HasSuffix(name, " SC") || strings.Contains(name, " SC ") {
			return f, nil
		}
		if first == nil {
			first = f
		}
	}
	if first == nil {
		return nil, errors.New("字体不含中文字形")
	}
	return first, nil
}

/* ---------------------------------- 版面 ---------------------------------- */

func render(f *sfnt.Font, c *model.Certificate, issuer, verifyLink, verifyPage string) *image.RGBA {
	cv := newCanvas(f)
	W := float64(pageW)

	// 边框与四角装饰
	cv.strokeRect(60, 60, W-60, pageH-60, 5, colorGold)
	cv.strokeRect(80, 80, W-80, pageH-80, 1.5, colorGold)
	for _, p := range [][2]float64{{80, 80}, {W - 80, 80}, {80, pageH - 80}, {W - 80, pageH - 80}} {
		cv.diamond(p[0], p[1], 16, colorGold)
	}

	// 抬头
	cv.text(issuer, W/2, 215, 28, colorGold, textOpt{align: alignCenter, spacing: 6})
	cv.text("音乐作品创作证明", W/2, 345, 88, colorInk, textOpt{align: alignCenter, bold: true, spacing: 12})
	cv.text("CERTIFICATE OF MUSIC CREATION", W/2, 410, 24, colorMuted, textOpt{align: alignCenter, spacing: 8})
	cv.fillRect(W/2-320, 456, W/2-30, 458, colorGold)
	cv.fillRect(W/2+30, 456, W/2+320, 458, colorGold)
	cv.diamond(W/2, 457, 12, colorGold)
	cv.text("证书编号  "+c.No, W/2, 522, 28, colorInk, textOpt{align: alignCenter, spacing: 2})

	// 正文
	y := 612.0
	intro := fmt.Sprintf("　　兹证明下列音乐作品由署名作者通过「%s」AI 音乐创作服务创作完成。"+
		"作品信息与音频文件指纹已于签发时登记存档，特此证明。", issuer)
	for _, line := range cv.wrap(intro, 30, W-2*margin) {
		cv.text(line, margin, y, 30, colorInk, textOpt{})
		y += 50
	}

	// 作品信息表：短字段两两并排，节省纵向空间给歌词
	y += 4
	cv.fillRect(margin, y, W-margin, y+2, colorGoldLight)
	half := margin + 700
	full := [2]float64{margin + 230, W - margin}
	left := [2]float64{margin + 230, half - 24}
	right := [2]float64{half + 150, W - margin}
	rows := [][]tableCell{
		{{"作品名称", "《" + c.Title + "》", 2, "", margin + 12, full}},
		{{"署名作者", c.Author, 1, "", margin + 12, full}},
		{{"作品 ID", c.SunoID, 1, "", margin + 12, full}},
		{
			{"创作完成时间", c.SongCreatedAt.Format("2006 年 01 月 02 日 15:04:05"), 1, "", margin + 12, left},
			{"签发时间", c.IssuedAt.Format("2006 年 01 月 02 日 15:04:05"), 1, "", half, right},
		},
		{
			{"作品时长", formatDuration(c.Duration), 1, "", margin + 12, left},
			{"生成模型", orDash(c.ModelName), 1, "", half, right},
		},
		{{"音乐风格", orDash(c.Tags), 2, "", margin + 12, full}},
		{{"音频指纹", groupHash(c.AudioSHA256), 2, hashNote(c.AudioSize), margin + 12, full}},
	}
	for _, row := range rows {
		bottom := y
		for _, cell := range row {
			bottom = math.Max(bottom, cv.cell(cell, y))
		}
		y = bottom
		cv.fillRect(margin, y, W-margin, y+1.5, colorRule)
	}

	// 核验二维码与签发信息的位置固定在页面下部，歌词填满中间的空间
	sigY := float64(pageH) - 500

	y += 72
	cv.fillRect(margin, y-28, margin+6, y+4, colorGold)
	if strings.TrimSpace(c.Lyrics) == "" {
		cv.text("歌词", margin+24, y, 30, colorInk, textOpt{bold: true})
		cv.text("纯音乐作品，无歌词", margin+24, y+60, 25, colorMuted, textOpt{})
	} else {
		truncated := cv.lyrics(c.Lyrics, margin+24, y+56, W-margin, sigY-80)
		title := "歌词"
		if truncated {
			title = "歌词（节选）"
		}
		cv.text(title, margin+24, y, 30, colorInk, textOpt{bold: true})
	}

	if verifyLink != "" {
		cv.qr(verifyLink, margin, sigY-40, 220)
		tx := margin + 250
		cv.text("扫码在线核验", tx, sigY+24, 28, colorInk, textOpt{})
		cv.text("或打开核验页面输入证书编号", tx, sigY+70, 22, colorMuted, textOpt{})
		page := verifyPage
		if i := strings.IndexByte(page, '?'); i >= 0 {
			page = page[:i]
		}
		for i, line := range cv.wrap(page, 22, 420) {
			if i == 2 {
				break
			}
			cv.text(line, tx, sigY+108+float64(i)*34, 22, colorMuted, textOpt{})
		}
	}
	cv.text("签发单位："+issuer, W-margin, sigY+30, 28, colorInk, textOpt{align: alignRight})
	cv.text("签发日期："+c.IssuedAt.Format("2006 年 01 月 02 日"), W-margin, sigY+90, 28, colorInk, textOpt{align: alignRight})
	// 印章压在签发日期末尾，与纸质证书的盖章位置一致
	cv.seal(W-margin-80, sigY+66, 118, issuer)

	// 说明
	y = float64(pageH) - 270
	cv.fillRect(margin, y, W-margin, y+1.5, colorRule)
	y += 46
	notes := []string{
		"1. 本证明依据平台系统记录出具，用于证明上述作品的创作时间、来源及签发时的音频文件指纹，不替代著作权登记证书。",
		"2. 音频指纹为作品 MP3 文件的 SHA-256 值，与平台下载的 MP3 一致，可用于核对文件是否被改动；重新编码或剪辑后指纹会变化。",
	}
	if verifyLink != "" {
		notes = append(notes, "3. 证书真伪以在线核验结果为准。")
	} else {
		notes = append(notes, "3. 如需核验证书真伪，请向签发单位提供证书编号。")
	}
	for _, note := range notes {
		for _, line := range cv.wrap(note, 22, W-2*margin) {
			cv.text(line, margin, y, 22, colorMuted, textOpt{})
			y += 36
		}
	}
	return cv.img
}

// tableCell 作品信息表的一格：标签、取值与取值所占的横向范围。
type tableCell struct {
	label    string
	value    string
	maxLines int
	note     string
	labelX   float64
	span     [2]float64
}

// cell 从 top 开始绘制一格，返回这一格的底部位置。
func (cv *canvas) cell(c tableCell, top float64) float64 {
	const size, lineH = 28.0, 42.0
	width := c.span[1] - c.span[0]
	lines := cv.wrap(c.value, size, width)
	if len(lines) > c.maxLines {
		lines = lines[:c.maxLines]
		lines[len(lines)-1] = cv.ellipsis(lines[len(lines)-1], size, width)
	}
	y := top + 44
	cv.text(c.label, c.labelX, y, 26, colorMuted, textOpt{})
	for i, line := range lines {
		cv.text(line, c.span[0], y+float64(i)*lineH, size, colorInk, textOpt{})
	}
	y += float64(len(lines)-1) * lineH
	if c.note != "" {
		y += 34
		cv.text(c.note, c.span[0], y, 20, colorMuted, textOpt{})
	}
	return math.Max(y+22, top+66)
}

// lyrics 把歌词分两栏排进 [top, bottom)，放不下时截断并提示，返回是否截断。
func (cv *canvas) lyrics(text string, left, top, right, bottom float64) bool {
	const size, lineH, gap = 24.0, 38.0, 60.0
	colW := (right - left - gap) / 2

	var lines []string
	blank := true
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			if !blank {
				lines = append(lines, "")
			}
			blank = true
			continue
		}
		blank = false
		lines = append(lines, cv.wrap(raw, size, colW)...)
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	perCol := int((bottom - top) / lineH)
	if perCol < 1 {
		return len(lines) > 0
	}
	truncated := len(lines) > perCol*2
	if truncated {
		lines = append(lines[:perCol*2-1], "……（完整歌词以平台存档为准）")
	}

	for col := 0; col < 2; col++ {
		start := col * perCol
		if start >= len(lines) {
			break
		}
		end := min(start+perCol, len(lines))
		chunk := lines[start:end]
		// 第二栏不以空行开头
		for len(chunk) > 0 && chunk[0] == "" {
			chunk = chunk[1:]
		}
		x := left + float64(col)*(colW+gap)
		for i, line := range chunk {
			ink := colorInk
			if strings.HasPrefix(line, "[") || strings.HasPrefix(line, "【") || strings.HasPrefix(line, "…") {
				ink = colorMuted
			}
			cv.text(line, x, top+float64(i)*lineH, size, ink, textOpt{})
		}
	}
	return truncated
}

func formatDuration(sec float64) string {
	if sec <= 0 {
		return "—"
	}
	total := int(math.Round(sec))
	return fmt.Sprintf("%d 分 %02d 秒", total/60, total%60)
}

// groupHash 把 64 位十六进制指纹按 8 位一组排成两行，便于人工比对。
func groupHash(h string) string {
	if len(h) != 64 {
		return h
	}
	var parts []string
	for i := 0; i < 64; i += 8 {
		parts = append(parts, h[i:i+8])
	}
	return strings.Join(parts[:4], " ") + "\n" + strings.Join(parts[4:], " ")
}

func hashNote(size int64) string {
	if size <= 0 {
		return "SHA-256"
	}
	return fmt.Sprintf("MP3 文件 SHA-256 · 大小 %.2f MB（%d 字节）", float64(size)/(1<<20), size)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

/* ---------------------------------- 画布 ---------------------------------- */

type textAlign int

const (
	alignLeft textAlign = iota
	alignCenter
	alignRight
)

type textOpt struct {
	align   textAlign
	bold    bool
	spacing float64 // 字间距（像素）
}

// canvas 一次绘制用的画布。字体 Face 不是并发安全的，每次绘制各建一套。
type canvas struct {
	img   *image.RGBA
	font  *sfnt.Font
	faces map[float64]font.Face
	buf   sfnt.Buffer
}

func newCanvas(f *sfnt.Font) *canvas {
	img := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	draw.Draw(img, img.Bounds(), image.NewUniform(colorPaper), image.Point{}, draw.Src)
	return &canvas{img: img, font: f, faces: map[float64]font.Face{}}
}

func (cv *canvas) face(size float64) font.Face {
	if f, ok := cv.faces[size]; ok {
		return f
	}
	f, err := opentype.NewFace(cv.font, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err) // 字体已在加载时校验过，这里只可能是参数错误
	}
	cv.faces[size] = f
	return f
}

func (cv *canvas) measure(s string, size float64) float64 {
	return float64(font.MeasureString(cv.face(size), s)) / 64
}

// text 以 (x, y) 为基线绘制单行文字，y 为基线位置。
func (cv *canvas) text(s string, x, y, size float64, col color.Color, opt textOpt) {
	width := cv.measure(s, size)
	if opt.spacing > 0 {
		n := utf8.RuneCountInString(s)
		if n > 1 {
			width += opt.spacing * float64(n-1)
		}
	}
	switch opt.align {
	case alignCenter:
		x -= width / 2
	case alignRight:
		x -= width
	}

	offsets := [][2]float64{{0, 0}}
	if opt.bold {
		// 字体只有常规字重，叠印几次模拟粗体
		d := size / 45
		offsets = [][2]float64{{0, 0}, {d, 0}, {0, d / 2}, {d, d / 2}}
	}
	for _, o := range offsets {
		d := font.Drawer{Dst: cv.img, Src: image.NewUniform(col), Face: cv.face(size)}
		if opt.spacing == 0 {
			d.Dot = fixed.Point26_6{X: toFixed(x + o[0]), Y: toFixed(y + o[1])}
			d.DrawString(s)
			continue
		}
		cx := x + o[0]
		for _, r := range s {
			d.Dot = fixed.Point26_6{X: toFixed(cx), Y: toFixed(y + o[1])}
			d.DrawString(string(r))
			cx += cv.measure(string(r), size) + opt.spacing
		}
	}
}

// wrap 按宽度折行：中文逐字断行，英文单词、数字尽量不拆开；显式换行保留。
func (cv *canvas) wrap(s string, size, width float64) []string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		var cur strings.Builder
		curW := 0.0
		flush := func() {
			lines = append(lines, strings.TrimRight(cur.String(), " "))
			cur.Reset()
			curW = 0
		}
		for _, tok := range tokenize(para) {
			w := cv.measure(tok, size)
			if curW+w > width && cur.Len() > 0 {
				flush()
				if tok == " " {
					continue
				}
			}
			if w > width {
				for _, r := range tok {
					rw := cv.measure(string(r), size)
					if curW+rw > width && cur.Len() > 0 {
						flush()
					}
					cur.WriteRune(r)
					curW += rw
				}
				continue
			}
			cur.WriteString(tok)
			curW += w
		}
		flush()
	}
	return lines
}

// ellipsis 截断到宽度内并补省略号。
func (cv *canvas) ellipsis(s string, size, width float64) string {
	runes := []rune(s)
	for len(runes) > 0 && cv.measure(string(runes)+"…", size) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// tokenize 把一行文字切成断行单位：连续的拉丁字符为一个词，空格与中文各自单独成词。
func tokenize(s string) []string {
	var out []string
	var word strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t':
			if word.Len() > 0 {
				out = append(out, word.String())
				word.Reset()
			}
			out = append(out, " ")
		case r < 0x2E80:
			word.WriteRune(r)
		default:
			if word.Len() > 0 {
				out = append(out, word.String())
				word.Reset()
			}
			out = append(out, string(r))
		}
	}
	if word.Len() > 0 {
		out = append(out, word.String())
	}
	return out
}

func (cv *canvas) fillRect(x0, y0, x1, y1 float64, col color.Color) {
	r := image.Rect(int(math.Round(x0)), int(math.Round(y0)), int(math.Round(x1)), int(math.Round(y1)))
	if r.Dy() == 0 {
		r.Max.Y++
	}
	draw.Draw(cv.img, r, image.NewUniform(col), image.Point{}, draw.Over)
}

func (cv *canvas) strokeRect(x0, y0, x1, y1, w float64, col color.Color) {
	cv.fillRect(x0, y0, x1, y0+w, col)
	cv.fillRect(x0, y1-w, x1, y1, col)
	cv.fillRect(x0, y0, x0+w, y1, col)
	cv.fillRect(x1-w, y0, x1, y1, col)
}

func (cv *canvas) diamond(x, y, r float64, col color.Color) {
	cv.fillPath(col, [][][2]float64{{{x, y - r}, {x + r, y}, {x, y + r}, {x - r, y}}})
}

// fillPath 填充若干闭合多边形，反向的轮廓形成镂空。
func (cv *canvas) fillPath(col color.Color, contours [][][2]float64) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range contours {
		for _, p := range c {
			minX, minY = math.Min(minX, p[0]), math.Min(minY, p[1])
			maxX, maxY = math.Max(maxX, p[0]), math.Max(maxY, p[1])
		}
	}
	ox, oy := math.Floor(minX), math.Floor(minY)
	w, h := int(math.Ceil(maxX-ox))+1, int(math.Ceil(maxY-oy))+1
	ras := vector.NewRasterizer(w, h)
	ras.DrawOp = draw.Over
	for _, c := range contours {
		for i, p := range c {
			if i == 0 {
				ras.MoveTo(float32(p[0]-ox), float32(p[1]-oy))
			} else {
				ras.LineTo(float32(p[0]-ox), float32(p[1]-oy))
			}
		}
		ras.ClosePath()
	}
	ras.Draw(cv.img, image.Rect(int(ox), int(oy), int(ox)+w, int(oy)+h), image.NewUniform(col), image.Point{})
}

func circle(cx, cy, r float64, reverse bool) [][2]float64 {
	const n = 180
	pts := make([][2]float64, n)
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(i) / n
		if reverse {
			a = -a
		}
		pts[i] = [2]float64{cx + r*math.Cos(a), cy + r*math.Sin(a)}
	}
	return pts
}

// seal 绘制圆形印章：外圈、中心五角星、沿上弧排列的签发单位与底部的章名。
func (cv *canvas) seal(cx, cy, r float64, issuer string) {
	cv.fillPath(colorSeal, [][][2]float64{circle(cx, cy, r, false), circle(cx, cy, r-9, true)})

	star := make([][2]float64, 10)
	for i := range star {
		rr := r * 0.25
		if i%2 == 1 {
			rr *= 0.382
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		star[i] = [2]float64{cx + rr*math.Cos(a), cy - r*0.02 + rr*math.Sin(a)}
	}
	cv.fillPath(colorSeal, [][][2]float64{star})

	runes := []rune(issuer)
	if len(runes) > 18 {
		runes = runes[:18]
	}
	size := r * 0.2
	if len(runes) > 12 {
		size = r * 0.17
	}
	baseR := r - 18 - size*0.88 // 字底朝向圆心，字顶离外圈留出间隙
	// 每个字占的弧长按字宽分配，拉丁字母与空格更窄；总弧度不超过 240°
	widths := make([]float64, len(runes))
	total := 0.0
	for i, ch := range runes {
		w := cv.measure(string(ch), size)
		if ch == ' ' {
			w = size * 0.3
		}
		widths[i] = (w + size*0.12) / baseR
		total += widths[i]
	}
	scale := 1.0
	if total > 4.2 {
		scale = 4.2 / total
	}
	a := -math.Pi/2 - total*scale/2
	for i, ch := range runes {
		mid := a + widths[i]*scale/2
		if ch != ' ' {
			cv.glyph(ch, size, cx+baseR*math.Cos(mid), cy+baseR*math.Sin(mid), mid+math.Pi/2, colorSeal)
		}
		a += widths[i] * scale
	}

	cv.text("创作证明专用章", cx, cy+r*0.6, r*0.15, colorSeal, textOpt{align: alignCenter, spacing: 2})
}

// glyph 绘制旋转后的单个字：(x, y) 为字的基线中点，angle 为顺时针旋转弧度。
func (cv *canvas) glyph(ch rune, size, x, y, angle float64, col color.Color) {
	gi, err := cv.font.GlyphIndex(&cv.buf, ch)
	if err != nil || gi == 0 {
		return
	}
	ppem := toFixed(size)
	adv, err := cv.font.GlyphAdvance(&cv.buf, gi, ppem, font.HintingNone)
	if err != nil {
		return
	}
	segs, err := cv.font.LoadGlyph(&cv.buf, gi, ppem, nil)
	if err != nil {
		return
	}

	half := float64(adv) / 64 / 2
	cos, sin := math.Cos(angle), math.Sin(angle)
	ext := size * 1.6
	ox, oy := math.Floor(x-ext), math.Floor(y-ext)
	w := int(2*ext) + 2
	pt := func(p fixed.Point26_6) (float32, float32) {
		lx, ly := float64(p.X)/64-half, float64(p.Y)/64
		return float32(x + lx*cos - ly*sin - ox), float32(y + lx*sin + ly*cos - oy)
	}

	ras := vector.NewRasterizer(w, w)
	ras.DrawOp = draw.Over
	for _, seg := range segs {
		switch seg.Op {
		case sfnt.SegmentOpMoveTo:
			ras.MoveTo(pt(seg.Args[0]))
		case sfnt.SegmentOpLineTo:
			ras.LineTo(pt(seg.Args[0]))
		case sfnt.SegmentOpQuadTo:
			bx, by := pt(seg.Args[0])
			cx, cy := pt(seg.Args[1])
			ras.QuadTo(bx, by, cx, cy)
		case sfnt.SegmentOpCubeTo:
			bx, by := pt(seg.Args[0])
			cx, cy := pt(seg.Args[1])
			dx, dy := pt(seg.Args[2])
			ras.CubeTo(bx, by, cx, cy, dx, dy)
		}
	}
	ras.ClosePath()
	ras.Draw(cv.img, image.Rect(int(ox), int(oy), int(ox)+w, int(oy)+w), image.NewUniform(col), image.Point{})
}

// qr 在 (x, y) 处绘制边长 size 的二维码，底下垫白色留白。
func (cv *canvas) qr(content string, x, y, size float64) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return
	}
	q.DisableBorder = true
	bm := q.Bitmap()
	n := len(bm)
	if n == 0 {
		return
	}
	pad := size * 0.06
	cv.fillRect(x-pad, y-pad, x+size+pad, y+size+pad, color.White)
	cell := size / float64(n)
	for row := 0; row < n; row++ {
		for col := 0; col < n; col++ {
			if bm[row][col] {
				// 按累计坐标取整，相邻模块之间不留缝
				cv.fillRect(x+float64(col)*cell, y+float64(row)*cell,
					x+float64(col+1)*cell, y+float64(row+1)*cell, colorInk)
			}
		}
	}
}

func toFixed(v float64) fixed.Int26_6 { return fixed.Int26_6(math.Round(v * 64)) }
