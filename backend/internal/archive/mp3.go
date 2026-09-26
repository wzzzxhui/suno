package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/lepro/suno-open-api/internal/media"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/storage"
)

// ErrNoAudio 作品音频已失效，无法生成 MP3。
var ErrNoAudio = errors.New("作品音频已失效")

// MP3Maker 为作品提供 MP3 下载文件。
//
// 上游字段虽叫 mp3Url，实际给的多是 M4A；这里把非 MP3 的音频用 ffmpeg 转成 MP3。
// 转码使用 bitexact 参数，同一份源文件转出的结果逐字节一致。已转存到 COS 的作品，
// 首次生成后再存一份 MP3 副本（archived.mp3），之后下载与创作证明指纹都用这份文件。
type MP3Maker struct {
	store    *storage.Store
	archiver *Archiver // 为 nil 时不缓存副本，每次现转
	ffmpeg   string
	client   *http.Client
	locks    sync.Map // 同一作品同时只转一次
}

// NewMP3Maker 创建 MP3 生成器，archiver 可为 nil。
func NewMP3Maker(store *storage.Store, archiver *Archiver, ffmpeg string) *MP3Maker {
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	return &MP3Maker{store: store, archiver: archiver, ffmpeg: ffmpeg, client: &http.Client{Timeout: 3 * time.Minute}}
}

// Get 返回作品的 MP3 文件内容。t 需带 FileInfo（含转存信息）。
func (m *MP3Maker) Get(ctx context.Context, t *model.Task) ([]byte, error) {
	mu, _ := m.locks.LoadOrStore(t.ID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	// 同一请求可能在等锁期间被别人生成好副本，重新读一次转存信息
	archived := m.archived(ctx, t)
	if archived != nil && archived.MP3 != "" {
		if data, err := m.download(ctx, m.archiver.Sign(archived.MP3, 10*time.Minute)); err == nil && isMP3(data) {
			return data, nil
		}
		log.Printf("[mp3] 副本读取失败，重新生成 task=%d", t.ID)
	}

	src := media.PlayableAudio(ctx, t)
	if src == "" {
		return nil, ErrNoAudio
	}
	data, err := m.download(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("下载作品音频: %w", err)
	}

	passthrough := isMP3(data)
	if !passthrough {
		if data, err = m.transcode(ctx, data); err != nil {
			return nil, err
		}
	}

	// 只给已转存的作品存副本；未转存的作品交给转存流程，避免提前写入 archived 让补转存跳过它
	if archived != nil {
		key := archived.Audio
		if !passthrough || key == "" {
			key = fmt.Sprintf("songs/m%d/%d.mp3", t.MerchantID, t.ID)
			if err := m.archiver.signer.Put(ctx, m.archiver.client, m.archiver.base+"/"+key, "audio/mpeg", data); err != nil {
				log.Printf("[mp3] 保存副本失败 task=%d: %v", t.ID, err)
				return data, nil
			}
		}
		archived.MP3 = key
		if err := m.store.SetArchived(ctx, t.ID, *archived); err != nil {
			log.Printf("[mp3] 记录副本失败 task=%d: %v", t.ID, err)
		}
	}
	return data, nil
}

// URL 返回作品 MP3 在 COS 上的签名地址，供唱歌克隆等外部服务下载。
//
// Suno 的「m4a」里装的是 Opus 编码，腾讯唱歌克隆解不了（报 10004 Engine Error），
// 所以交给外部服务前统一转成 MP3。已转存的作品复用 archived.mp3 副本，未转存的也写到同一路径。
func (m *MP3Maker) URL(ctx context.Context, t *model.Task, expire time.Duration) (string, error) {
	if m.archiver == nil {
		return "", errors.New("未配置 COS，无法提供 MP3 地址")
	}
	if a := m.archived(ctx, t); a != nil && a.MP3 != "" {
		return m.archiver.Sign(a.MP3, expire), nil
	}
	data, err := m.Get(ctx, t)
	if err != nil {
		return "", err
	}
	if a := m.archived(ctx, t); a != nil && a.MP3 != "" {
		return m.archiver.Sign(a.MP3, expire), nil
	}
	key := fmt.Sprintf("songs/m%d/%d.mp3", t.MerchantID, t.ID)
	if err := m.archiver.signer.Put(ctx, m.archiver.client, m.archiver.base+"/"+key, "audio/mpeg", data); err != nil {
		return "", fmt.Errorf("上传 MP3 失败: %w", err)
	}
	return m.archiver.Sign(key, expire), nil
}

// archived 读取最新的转存信息；未配置 COS 或作品未转存时返回 nil。
func (m *MP3Maker) archived(ctx context.Context, t *model.Task) *model.ArchivedFiles {
	if m.archiver == nil {
		return nil
	}
	if row, _, err := m.store.AdminTaskByID(ctx, t.ID); err == nil && row.FileInfo != nil {
		t.FileInfo = row.FileInfo
	}
	if t.FileInfo == nil || t.FileInfo.Archived == nil || t.FileInfo.Archived.Missing {
		return nil
	}
	a := *t.FileInfo.Archived
	return &a
}

func (m *MP3Maker) download(ctx context.Context, src string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxFileSize {
		return nil, fmt.Errorf("文件为空或超过 100MB")
	}
	return data, nil
}

// Transcode 把任意音频转成 MP3，供上传演唱音色等场景使用（浏览器录音是 WAV）。
func (m *MP3Maker) Transcode(ctx context.Context, data []byte) ([]byte, error) {
	if isMP3(data) {
		return data, nil
	}
	return m.transcode(ctx, data)
}

// transcode 用 ffmpeg 把音频转成 192kbps MP3。M4A 的索引常在文件末尾，不能走管道输入，先落临时文件。
func (m *MP3Maker) transcode(ctx context.Context, data []byte) ([]byte, error) {
	in, err := os.CreateTemp("", "mp3src-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(data); err != nil {
		in.Close()
		return nil, err
	}
	in.Close()

	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, m.ffmpeg, "-hide_banner", "-loglevel", "error", "-i", in.Name(),
		"-vn", "-map_metadata", "-1", "-c:a", "libmp3lame", "-b:a", "192k",
		// 去掉编码器版本等可变信息，保证同一源文件每次转出的字节相同
		"-fflags", "+bitexact", "-flags:a", "+bitexact", "-id3v2_version", "0", "-write_id3v1", "0",
		"-f", "mp3", "pipe:1")
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("转码 MP3 失败: %v %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("转码 MP3 失败: 输出为空")
	}
	return out.Bytes(), nil
}

// isMP3 按文件头判断是否已是 MP3：ID3 标签，或 MPEG Layer III 帧同步。
// AAC 的 ADTS 帧同步与 MP3 相似，靠 layer 位区分（ADTS 恒为 00）。
func isMP3(data []byte) bool {
	if len(data) >= 3 && string(data[:3]) == "ID3" {
		return true
	}
	return len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0 && (data[1]>>1)&0x03 == 0x01
}
