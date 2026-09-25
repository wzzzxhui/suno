package certificate

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// A4 尺寸（单位：pt）。
const (
	a4Width  = 595.28
	a4Height = 841.89
)

type pdfInfo struct {
	Title    string
	Author   string
	Subject  string
	Keywords string
	Created  time.Time
}

// buildPDF 生成只有一页的 PDF：整页铺一张 JPEG。图片直接以 DCTDecode 嵌入，不重新编码。
func buildPDF(jpegData []byte, width, height int, info pdfInfo) []byte {
	content := fmt.Sprintf("q\n%.2f 0 0 %.2f 0 0 cm\n/Im0 Do\nQ\n", a4Width, a4Height)
	created := pdfDate(info.Created)

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] "+
			"/Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>", a4Width, a4Height),
		"", // 图片，单独写出二进制流
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		fmt.Sprintf("<< /Title %s /Author %s /Subject %s /Keywords %s /Creator %s /Producer (suno-open-api) "+
			"/CreationDate (%s) /ModDate (%s) >>",
			pdfText(info.Title), pdfText(info.Author), pdfText(info.Subject), pdfText(info.Keywords),
			pdfText(info.Author), created, created),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n", i+1)
		if i == 3 {
			fmt.Fprintf(&buf, "<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB "+
				"/BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", width, height, len(jpegData))
			buf.Write(jpegData)
			buf.WriteString("\nendstream")
		} else {
			buf.WriteString(body)
		}
		buf.WriteString("\nendobj\n")
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// pdfText 把任意文字编码成 PDF 的 UTF-16BE 十六进制字符串，中文元数据才能正确显示。
func pdfText(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", u)
	}
	b.WriteString(">")
	return b.String()
}

// pdfDate 格式化为 PDF 日期：D:YYYYMMDDHHmmSS+08'00'。
func pdfDate(t time.Time) string {
	_, offset := t.Zone()
	sign := '+'
	if offset < 0 {
		sign, offset = '-', -offset
	}
	return fmt.Sprintf("D:%s%c%02d'%02d'", t.Format("20060102150405"), sign, offset/3600, offset%3600/60)
}
