package task

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/provider"
	"github.com/lepro/suno-open-api/internal/storage"
)

// SamplePrefix 后台上传的音频在 COS 里的目录，只清理这个目录下的文件。
const SamplePrefix = "voice_samples/"

// SampleCleaner 在音色训练、翻唱结束后删除为它上传到 COS 的音频。
// 唱歌克隆在任务开始时就已下载音频，结束后这份文件不再有用。
type SampleCleaner struct {
	store  *storage.Store
	signer *provider.COSSigner
	prefix string
	client *http.Client
}

// NewSampleCleaner 创建清理器；base 为存储桶根地址，signer 未配置密钥时返回 nil。
func NewSampleCleaner(store *storage.Store, signer *provider.COSSigner, base string) *SampleCleaner {
	if !signer.Enabled() || base == "" {
		return nil
	}
	return &SampleCleaner{
		store:  store,
		signer: signer,
		prefix: strings.TrimRight(base, "/") + "/" + SamplePrefix,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Clean 处理一个已进入终态的任务。
func (c *SampleCleaner) Clean(ctx context.Context, t *model.Task) {
	if c == nil || (t.Kind != model.KindVoiceTrain && t.Kind != model.KindVoiceCover) {
		return
	}

	_, raw, err := c.store.AdminTaskByID(ctx, t.ID)
	if err != nil {
		return
	}
	var payload struct {
		AudioURL string `json:"audio_url"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || payload.AudioURL == "" {
		return
	}

	// 只删平台自己上传的文件；运营粘贴的外部链接与作品库歌曲不动
	objectURL := payload.AudioURL
	if i := strings.IndexByte(objectURL, '?'); i >= 0 {
		objectURL = objectURL[:i]
	}
	if !strings.HasPrefix(objectURL, c.prefix) {
		return
	}

	// 同一份音频被重复提交时，等最后一个任务结束再删
	if inUse, err := c.store.AudioInUse(ctx, payload.AudioURL, t.ID); err != nil || inUse {
		return
	}

	if err := c.signer.Delete(ctx, c.client, objectURL); err != nil {
		log.Printf("[cleanup] 删除上传音频失败 task=%d: %v", t.ID, err)
		return
	}
	log.Printf("[cleanup] 已删除上传音频 task=%d %s", t.ID, strings.TrimPrefix(objectURL, c.prefix))
}
