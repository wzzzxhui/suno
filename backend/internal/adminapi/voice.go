package adminapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/musicreq"
	"github.com/lepro/suno-open-api/internal/provider"
	"github.com/lepro/suno-open-api/internal/storage"
	"github.com/lepro/suno-open-api/internal/task"
)

/* ---------------------------------- 音色翻唱 ---------------------------------- */

// 唱歌克隆内置的通用音色，无需训练即可使用
const (
	defaultVoiceModel = "default"
	defaultVoiceName  = "官方默认音色"
	maxTrainEpoch     = 50
)

// voices 列出音色库。
func (s *Server) voices(w http.ResponseWriter, r *http.Request) error {
	list, err := s.store.ListVoices(r.Context(), int64(httpx.QueryInt(r, "merchant_id", 0)))
	if err != nil {
		return err
	}

	// 上游不给进度百分比，界面按「每轮耗时 × 轮次」估算；有完成记录时用实际平均值
	pace := map[string]interface{}{"seconds_per_epoch": s.cfg.VoiceTrainSecondsPerEpoch, "samples": 0}
	if avg, n, err := s.store.VoiceTrainPace(r.Context()); err == nil && n > 0 && avg > 0 {
		pace = map[string]interface{}{"seconds_per_epoch": avg, "samples": n}
	}
	httpx.JSON(w, map[string]interface{}{
		"list":       list,
		"configured": s.cfg.VoiceConfigured(),
		// 训练超过该时长仍未完成会被判失败，界面用来提示预计等待
		"train_timeout_minutes": int(s.cfg.VoiceTrainTimeout.Minutes()),
		"train_pace":            pace,
		"default":               map[string]string{"model_name": defaultVoiceModel, "name": defaultVoiceName},
		"prices": map[string]int64{
			"train": model.PriceOf(model.KindVoiceTrain),
			"cover": model.PriceOf(model.KindVoiceCover),
		},
	})
	return nil
}

type voiceTrainRequest struct {
	MerchantID int64  `json:"merchant_id"`
	Name       string `json:"name"`
	AudioURL   string `json:"audio_url"`
	TotalEpoch int    `json:"total_epoch"`
}

// trainVoice 上传一段唱歌干声，训练出该商户专属的音色模型。
func (s *Server) trainVoice(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req voiceTrainRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 30 {
		return httpx.BadRequest("音色名称为 1~30 个字符")
	}
	if err := requireHTTPURL(req.AudioURL); err != nil {
		return err
	}
	if req.TotalEpoch < 0 || req.TotalEpoch > maxTrainEpoch {
		return httpx.BadRequest(fmt.Sprintf("训练轮次为 1~%d，留空默认 30", maxTrainEpoch))
	}

	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}

	// 模型名在腾讯账号内全局唯一，只能用字母、数字与下划线
	payload := map[string]interface{}{
		"name":       req.Name,
		"model_name": fmt.Sprintf("m%d_%s", merchant.ID, randomToken(8)),
		"audio_url":  strings.TrimSpace(req.AudioURL),
		"created_by": "admin:" + user.Username,
	}
	if req.TotalEpoch > 0 {
		payload["total_epoch"] = req.TotalEpoch
	}

	return s.submitVoiceTask(w, r, merchant.ID, model.KindVoiceTrain, payload)
}

// deleteVoice 从音色库移除音色。
func (s *Server) deleteVoice(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.ID <= 0 {
		return httpx.BadRequest("缺少音色 ID")
	}
	if err := s.store.DeleteVoice(r.Context(), req.ID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("音色不存在或已删除")
		}
		return err
	}
	httpx.JSON(w, map[string]interface{}{"deleted": true})
	return nil
}

type voiceCoverRequest struct {
	MerchantID int64 `json:"merchant_id"`
	// VoiceID 为音色库里的训练任务编号，0 表示使用官方默认音色
	VoiceID int64 `json:"voice_id"`
	// 原曲二选一：作品库里的歌曲任务编号，或可公开下载的音频地址
	SongTaskID int64  `json:"song_task_id"`
	AudioURL   string `json:"audio_url"`
	Separate   bool   `json:"separate"`
	Title      string `json:"title"`
}

// coverWithVoice 用指定音色重唱一首歌：保留原曲旋律与伴奏，只替换人声音色。
func (s *Server) coverWithVoice(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}

	var req voiceCoverRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}

	modelName, voiceName := defaultVoiceModel, defaultVoiceName
	if req.VoiceID > 0 {
		if modelName, voiceName, err = s.voiceOf(r, req.VoiceID, merchant.ID); err != nil {
			return err
		}
	}

	audioURL := strings.TrimSpace(req.AudioURL)
	if req.SongTaskID > 0 {
		if audioURL, err = s.songAudioOf(r, req.SongTaskID, merchant.ID); err != nil {
			return err
		}
	} else if err := requireHTTPURL(audioURL); err != nil {
		return err
	}

	payload := map[string]interface{}{
		"audio_url":  audioURL,
		"model_name": modelName,
		"voice_id":   req.VoiceID,
		"voice_name": voiceName,
		"separate":   req.Separate,
		"title":      strings.TrimSpace(req.Title),
		"created_by": "admin:" + user.Username,
	}
	if req.SongTaskID > 0 {
		payload["song_task_id"] = req.SongTaskID
	}

	return s.submitVoiceTask(w, r, merchant.ID, model.KindVoiceCover, payload)
}

type vocalCoverRequest struct {
	MerchantID int64  `json:"merchant_id"`
	VoiceID    int64  `json:"voice_id"`
	SongTaskID int64  `json:"song_task_id"`
	AudioURL   string `json:"audio_url"`
	Title      string `json:"title"`
	Lyrics     string `json:"lyrics"`
}

// coverWithVocal 参考原曲并指定演唱音色重新生成歌曲；上游不会保留原伴奏。
func (s *Server) coverWithVocal(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}
	var req vocalCoverRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}
	if req.VoiceID <= 0 {
		return httpx.BadRequest("请选择已创建的高级演唱音色")
	}
	if _, _, err := s.vocalOf(r, req.VoiceID, merchant.ID); err != nil {
		return err
	}
	title := strings.TrimSpace(req.Title)
	lyrics := strings.TrimSpace(req.Lyrics)
	if title == "" || utf8.RuneCountInString(title) > 100 {
		return httpx.BadRequest("歌名需为 1~100 个字符")
	}
	if lyrics == "" || utf8.RuneCountInString(lyrics) > murekaLyricsMax {
		return httpx.BadRequest("请填写 1~5000 个字符的歌词")
	}
	if (req.SongTaskID > 0) == (strings.TrimSpace(req.AudioURL) != "") {
		return httpx.BadRequest("作品库歌曲和音频链接只能选择一个")
	}
	audioURL := strings.TrimSpace(req.AudioURL)
	if req.SongTaskID > 0 {
		if audioURL, err = s.songAudioOf(r, req.SongTaskID, merchant.ID); err != nil {
			return err
		}
	} else if err := requireHTTPURL(audioURL); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	referenceID, err := s.vocal.UploadURL(ctx, "reference", audioURL)
	if err != nil {
		return httpx.BadRequest("上传原曲参考失败：" + err.Error())
	}
	generate := &adminGenerateRequest{
		Generate:    musicreq.Generate{Title: title, Prompt: lyrics},
		MerchantID:  merchant.ID,
		Provider:    "mureka",
		VoiceID:     req.VoiceID,
		ReferenceID: referenceID,
		MurekaModel: "auto",
	}
	return s.generateMureka(w, r, user, merchant, generate)
}

// voiceOf 取出音色的模型名，并确认音色已训练完成且属于该商户。
func (s *Server) voiceOf(r *http.Request, voiceID, merchantID int64) (string, string, error) {
	t, raw, err := s.store.AdminTaskByID(r.Context(), voiceID)
	if err != nil || t.Task.Kind != model.KindVoiceTrain {
		if err == nil || errors.Is(err, storage.ErrNotFound) {
			return "", "", httpx.NotFound("音色不存在")
		}
		return "", "", err
	}
	if t.MerchantID != merchantID {
		return "", "", httpx.BadRequest("该音色不属于所选商户")
	}
	if t.Status != model.StatusCompleted {
		return "", "", httpx.BadRequest("该音色尚未训练完成")
	}

	var payload struct {
		Name      string `json:"name"`
		ModelName string `json:"model_name"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.ModelName == "" {
		return "", "", httpx.Internal("音色数据缺少模型名")
	}
	return payload.ModelName, payload.Name, nil
}

// songAudioOf 取出作品的音频地址，作为翻唱原曲。
func (s *Server) songAudioOf(r *http.Request, taskID, merchantID int64) (string, error) {
	t, _, err := s.store.AdminTaskByID(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return "", httpx.NotFound("作品不存在")
		}
		return "", err
	}
	if t.MerchantID != merchantID {
		return "", httpx.BadRequest("该作品不属于所选商户")
	}
	if t.Status != model.StatusCompleted {
		return "", httpx.BadRequest("作品尚未生成完成")
	}

	// 转成 MP3 放到 COS（Suno 原文件是 Opus，唱歌克隆解不了）；转不了时退回当前能访问的原始音频
	if u := s.tasks.CoverAudio(r.Context(), &t.Task); u != "" {
		return u, nil
	}
	return "", httpx.BadRequest("该作品的音频已失效")
}

// submitVoiceTask 提交音色类任务并返回扣费后的余额。
func (s *Server) submitVoiceTask(w http.ResponseWriter, r *http.Request, merchantID int64, kind model.TaskKind, payload map[string]interface{}) error {
	ids, err := s.tasks.Submit(r.Context(), merchantID, kind, payload, 0)
	if err != nil {
		return err
	}
	balance, _ := s.store.Balance(r.Context(), merchantID)
	httpx.JSON(w, map[string]interface{}{
		"task_ids": ids,
		"balance":  balance,
		"cost":     model.PriceOf(kind),
	})
	return nil
}

func requireHTTPURL(u string) error {
	u = strings.TrimSpace(u)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return httpx.BadRequest("音频地址必须是可公开访问的 http/https 链接")
	}
	return nil
}

func randomToken(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

/* ---------------------------------- 音频上传 ---------------------------------- */

const maxSampleSize = 50 << 20 // 单个音频上限 50MB

// 允许的音频格式；浏览器录音会先在前端转成 wav
var sampleTypes = map[string]string{
	".wav":  "audio/wav",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",
}

// uploadSample 接收浏览器上传的音频（文件或现场录音），存进 COS 并返回可供唱歌克隆下载的地址。
func (s *Server) uploadSample(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxSampleSize+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return httpx.BadRequest("音频不能超过 50MB")
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	merchantID, _ := strconv.ParseInt(r.FormValue("merchant_id"), 10, 64)
	merchant, err := s.activeMerchant(r, merchantID)
	if err != nil {
		return err
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("请选择要上传的音频文件")
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	contentType, ok := sampleTypes[ext]
	if !ok {
		return httpx.BadRequest("仅支持 wav、mp3、m4a、aac、flac、ogg 格式")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSampleSize+1))
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > maxSampleSize {
		return httpx.BadRequest("音频为空或超过 50MB")
	}

	key := fmt.Sprintf("%sm%d/%s/%s%s", task.SamplePrefix, merchant.ID, time.Now().Format("20060102"), randomToken(8), ext)
	url, err := s.storeSample(r, key, contentType, data)
	if err != nil {
		return err
	}
	httpx.JSON(w, map[string]interface{}{"url": url, "name": header.Filename, "size": len(data)})
	return nil
}

// storeSample 写入 COS 并返回签名下载地址；未配置 COS 时返回模拟地址，便于联调界面。
func (s *Server) storeSample(r *http.Request, key, contentType string, data []byte) (string, error) {
	signer := &provider.COSSigner{
		SecretID:  s.cfg.VoiceCOSSecretID,
		SecretKey: s.cfg.VoiceCOSSecretKey,
		Expire:    s.cfg.VoiceURLExpire,
	}
	if !signer.Enabled() || s.cfg.VoiceOutputBase == "" {
		if s.cfg.VoiceConfigured() {
			return "", httpx.BadRequest("未配置 TME_COS_* 存储桶与密钥，无法上传音频")
		}
		return s.cfg.MockCDNBase + "/" + key, nil
	}

	objectURL := s.cfg.VoiceOutputBase + "/" + key
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	if err := signer.Put(ctx, &http.Client{}, objectURL, contentType, data); err != nil {
		return "", httpx.Internal(err.Error())
	}
	return signer.Sign(objectURL, time.Now())
}
