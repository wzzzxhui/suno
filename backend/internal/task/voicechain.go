package task

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/lepro/suno-open-api/internal/media"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/storage"
)

// VoicePayloadKey 生成音乐请求里记录所选音色的字段。只在本平台内部使用，提交上游前会去掉。
const VoicePayloadKey = "voice"

// VoiceChoice 创作时选定的音色：歌曲生成完成后用它自动翻唱。
type VoiceChoice struct {
	VoiceID   int64  `json:"voice_id"` // 音色库里的训练任务编号，0 为官方默认音色
	ModelName string `json:"model_name"`
	Name      string `json:"name"`
}

// chainVoiceCover 生成音乐时选了音色的，歌曲完成后自动提交一次音色翻唱：保留伴奏，只替换人声。
// 翻唱照常扣费；提交不成功（如积分不足）时记一条失败的翻唱任务，界面上能看到原因。
func (s *Service) chainVoiceCover(ctx context.Context, taskID int64) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	row, raw, err := s.store.AdminTaskByID(ctx, taskID)
	if err != nil || row.Kind != string(model.KindGenerate) {
		return
	}
	var req struct {
		Title     string       `json:"title"`
		CreatedBy string       `json:"created_by"`
		Voice     *VoiceChoice `json:"voice"`
	}
	if json.Unmarshal([]byte(raw), &req) != nil || req.Voice == nil || req.Voice.ModelName == "" {
		return
	}
	// 失败重试后会再次完成，已经翻唱过的不重复发起
	if _, err := s.store.VoiceCoverOf(ctx, taskID); err == nil {
		return
	} else if !errors.Is(err, storage.ErrNotFound) {
		log.Printf("[voice] 查询自动翻唱失败 song=%d: %v", taskID, err)
		return
	}

	payload := map[string]interface{}{
		"model_name":   req.Voice.ModelName,
		"voice_id":     req.Voice.VoiceID,
		"voice_name":   req.Voice.Name,
		"separate":     true,
		"title":        req.Title,
		"song_task_id": taskID,
		"created_by":   req.CreatedBy,
		"auto":         true,
	}

	audio := media.PlayableAudio(ctx, &row.Task)
	if audio == "" {
		s.recordChainFailure(ctx, row.MerchantID, payload, "歌曲音频不可用，未能自动用音色翻唱")
		return
	}
	payload["audio_url"] = audio

	ids, err := s.Submit(ctx, row.MerchantID, model.KindVoiceCover, payload, 0)
	if err != nil {
		s.recordChainFailure(ctx, row.MerchantID, payload, "自动音色翻唱未能提交："+err.Error())
		return
	}
	log.Printf("[voice] 歌曲完成，已用音色「%s」自动翻唱 song=%d cover=%v", req.Voice.Name, taskID, ids)
}

func (s *Service) recordChainFailure(ctx context.Context, merchantID int64, payload map[string]interface{}, reason string) {
	raw, _ := json.Marshal(payload)
	_, err := s.store.CreateFailedTask(ctx, &model.Task{
		MerchantID: merchantID,
		Kind:       model.KindVoiceCover,
		ExtraParam: remarkOf(model.KindVoiceCover),
		Request:    string(raw),
	}, reason)
	if err != nil {
		log.Printf("[voice] 记录自动翻唱失败出错: %v", err)
	}
	log.Printf("[voice] %s song=%v", reason, payload["song_task_id"])
}
