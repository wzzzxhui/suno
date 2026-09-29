package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lepro/suno-open-api/internal/httpx"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/provider"
	"github.com/lepro/suno-open-api/internal/storage"
)

/* ---------------------------------- 演唱音色 ---------------------------------- */
// 演唱音色走 Mureka：上传一段清唱得到 vocal_id，创作时直接用这个声音演唱新歌。
// 与「音色翻唱」（腾讯唱歌克隆，给已有的歌换人声）是两套音色。

const (
	maxVocalSample   = 10 << 20 // Mureka 要求小于 10MB
	murekaLyricsMax  = 5000
	murekaPromptMax  = 1024
	vocalNameMaxRune = 30
)

// SetVocal 设置演唱音色服务（Mureka 或本地模拟）。
func (s *Server) SetVocal(v provider.VocalService) { s.vocal = v }

func (s *Server) registerVocals(r *httpx.Router, auth httpx.Middleware) {
	r.GET("/admin/api/vocals", s.vocals, auth)
	r.POST("/admin/api/vocals/create", s.createVocal, auth)
	r.POST("/admin/api/vocals/delete", s.deleteVocal, auth)
	r.POST("/admin/api/mureka/upload", s.murekaUpload, auth)
	r.POST("/admin/api/mureka/lyrics", s.murekaLyrics, auth)
	r.POST("/admin/api/mureka/tool", s.murekaTool, auth)
	r.POST("/admin/api/mureka/upload-file", s.murekaUploadFile, auth)
	r.POST("/admin/api/mureka/upload-part", s.murekaUploadPart, auth)
}

const maxMurekaUploadFile = 10 << 20

func (s *Server) murekaUploadFile(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxMurekaUploadFile+1<<20)
	if err := r.ParseMultipartForm(maxMurekaUploadFile); err != nil {
		return httpx.BadRequest("文件不能超过 10MB")
	}
	purpose := r.FormValue("purpose")
	switch purpose {
	case "reference", "melody", "instrumental", "voice", "audio", "remix", "soundtrack", "lyrics-video":
	default:
		return httpx.BadRequest("不支持的文件用途")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("请选择上传文件")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxMurekaUploadFile+1))
	if err != nil || len(data) == 0 || len(data) > maxMurekaUploadFile {
		return httpx.BadRequest("文件为空或超过 10MB")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	id, err := s.vocal.UploadFile(ctx, purpose, header.Filename, data)
	if err != nil {
		return httpx.BadRequest("上传高级模式素材失败：" + err.Error())
	}
	httpx.JSON(w, map[string]interface{}{"id": id, "purpose": purpose})
	return nil
}

func (s *Server) murekaUploadPart(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxMurekaUploadFile+1<<20)
	if err := r.ParseMultipartForm(maxMurekaUploadFile); err != nil {
		return httpx.BadRequest("文件块不能超过 10MB")
	}
	uploadID := strings.TrimSpace(r.FormValue("upload_id"))
	if uploadID == "" {
		return httpx.BadRequest("请填写上传 ID")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("请选择文件块")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxMurekaUploadFile+1))
	if err != nil || len(data) == 0 || len(data) > maxMurekaUploadFile {
		return httpx.BadRequest("文件块为空或超过 10MB")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	raw, err := s.vocal.UploadPart(ctx, uploadID, header.Filename, data)
	if err != nil {
		return httpx.BadRequest("追加文件块失败：" + err.Error())
	}
	httpx.JSON(w, map[string]interface{}{"result": json.RawMessage(raw)})
	return nil
}

type murekaToolRequest struct {
	Operation string                 `json:"operation"`
	TaskID    string                 `json:"task_id"`
	Params    map[string]interface{} `json:"params"`
}

func (s *Server) murekaTool(w http.ResponseWriter, r *http.Request) error {
	var req murekaToolRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	raw, err := s.vocal.CallTool(ctx, req.Operation, req.TaskID, req.Params)
	if err != nil {
		return httpx.BadRequest("高级工具执行失败：" + err.Error())
	}
	httpx.JSON(w, map[string]interface{}{"result": json.RawMessage(raw)})
	return nil
}

type murekaUploadRequest struct {
	Purpose string `json:"purpose"`
	URL     string `json:"url"`
}

func (s *Server) murekaUpload(w http.ResponseWriter, r *http.Request) error {
	var req murekaUploadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	switch req.Purpose {
	case "reference", "melody", "instrumental", "voice", "audio", "remix", "soundtrack", "lyrics-video":
	default:
		return httpx.BadRequest("不支持的文件用途")
	}
	if err := requireHTTPURL(req.URL); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	id, err := s.vocal.UploadURL(ctx, req.Purpose, strings.TrimSpace(req.URL))
	if err != nil {
		return httpx.BadRequest("上传高级模式素材失败：" + err.Error())
	}
	httpx.JSON(w, map[string]interface{}{"id": id, "purpose": req.Purpose})
	return nil
}

type murekaLyricsRequest struct {
	Prompt string `json:"prompt"`
}

func (s *Server) murekaLyrics(w http.ResponseWriter, r *http.Request) error {
	var req murekaLyricsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return httpx.BadRequest("请填写歌词描述")
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	title, lyrics, err := s.vocal.GenerateLyrics(ctx, strings.TrimSpace(req.Prompt))
	if err != nil {
		return httpx.BadRequest("生成歌词失败：" + err.Error())
	}
	httpx.JSON(w, map[string]interface{}{"title": title, "lyrics": lyrics})
	return nil
}

// vocals 列出商户的演唱音色。
func (s *Server) vocals(w http.ResponseWriter, r *http.Request) error {
	list, err := s.store.ListVoicesOf(r.Context(), model.KindVoiceClone, int64(httpx.QueryInt(r, "merchant_id", 0)))
	if err != nil {
		return err
	}
	if s.cfg.MurekaConfigured() {
		valid := list[:0]
		for _, v := range list {
			if !strings.HasPrefix(v.ModelName, "mock-") {
				valid = append(valid, v)
			}
		}
		list = valid
	}
	httpx.JSON(w, map[string]interface{}{
		"list":       list,
		"configured": s.cfg.MurekaConfigured(),
		"prices": map[string]int64{
			"clone": model.PriceOf(model.KindVoiceClone),
			"song":  model.PriceOf(model.KindVoiceSong),
		},
	})
	return nil
}

type createVocalRequest struct {
	MerchantID int64  `json:"merchant_id"`
	Name       string `json:"name"`
	AudioURL   string `json:"audio_url"`
}

// createVocal 用一段 15~30 秒清唱创建演唱音色：取回音频，非 MP3 先转码（浏览器录音是 WAV），交给 Mureka 克隆。
func (s *Server) createVocal(w http.ResponseWriter, r *http.Request) error {
	user, err := adminFrom(r)
	if err != nil {
		return err
	}
	var req createVocalRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > vocalNameMaxRune {
		return httpx.BadRequest(fmt.Sprintf("音色名称为 1~%d 个字符", vocalNameMaxRune))
	}
	if err := requireHTTPURL(req.AudioURL); err != nil {
		return err
	}
	merchant, err := s.activeMerchant(r, req.MerchantID)
	if err != nil {
		return err
	}

	// 下载、转码、上传 Mureka 可能要十几秒，不跟随浏览器请求取消
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()

	data, err := fetchSample(ctx, strings.TrimSpace(req.AudioURL))
	if err != nil {
		return httpx.BadRequest("读取清唱音频失败：" + err.Error())
	}
	if s.mp3 != nil {
		if data, err = s.mp3.Transcode(ctx, data); err != nil {
			return httpx.BadRequest("音频格式无法识别，请上传 mp3 / m4a / wav 清唱")
		}
	}
	if len(data) > maxVocalSample {
		return httpx.BadRequest("清唱音频转成 MP3 后超过 10MB，请截取 15~30 秒的片段")
	}

	price := model.PriceOf(model.KindVoiceClone)
	vocalID, err := s.vocal.CloneVocal(ctx, req.Name, "vocal.mp3", data)
	if err != nil {
		return httpx.BadRequest("创建演唱音色失败：" + err.Error())
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"name":       req.Name,
		"vocal_id":   vocalID,
		"sample_url": strings.TrimSpace(req.AudioURL),
		"created_by": "admin:" + user.Username,
	})
	t := &model.Task{
		MerchantID: merchant.ID, Kind: model.KindVoiceClone, ProviderTaskID: vocalID,
		ExtraParam: model.LabelOf(model.KindVoiceClone), PointsCost: price, Request: string(raw),
	}
	id, err := s.store.CreateTask(ctx, t)
	if err != nil {
		return err
	}
	if err := s.store.CompleteTask(ctx, id, "", "", "", nil); err != nil {
		return err
	}
	balance, _ := s.store.Balance(ctx, merchant.ID)
	httpx.JSON(w, map[string]interface{}{"task_id": id, "vocal_id": vocalID, "cost": price, "balance": balance})
	return nil
}

// deleteVocal 从列表移除演唱音色。
func (s *Server) deleteVocal(w http.ResponseWriter, r *http.Request) error {
	var req idRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := s.store.DeleteVoiceOf(r.Context(), model.KindVoiceClone, req.ID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return httpx.NotFound("演唱音色不存在或已删除")
		}
		return err
	}
	httpx.JSON(w, map[string]interface{}{"deleted": true})
	return nil
}

// generateMureka 提交高级模式创作；演唱音色可选，所有外部 ID 只提交给 Mureka。
func (s *Server) generateMureka(w http.ResponseWriter, r *http.Request, user *model.AdminUser,
	merchant *model.Merchant, req *adminGenerateRequest) error {
	title := strings.TrimSpace(req.Title)
	if title == "" || utf8.RuneCountInString(title) > 100 {
		return httpx.BadRequest("歌名需为 1~100 个字符")
	}
	op := req.Task
	switch op {
	case "", "instrumental", "extend", "remix":
	default:
		return httpx.BadRequest("高级模式不支持该创作方式")
	}
	if req.MakeInstrumental != nil && *req.MakeInstrumental && op == "" {
		op = "instrumental"
	}
	if op != "" && op != "instrumental" && req.VoiceID > 0 {
		return httpx.BadRequest("续写与混音不能指定演唱音色")
	}
	if op == "instrumental" && req.VoiceID > 0 {
		return httpx.BadRequest("纯音乐不能指定演唱音色")
	}

	if req.ExcludeRap && (op == "extend" || strings.TrimSpace(req.ReferenceID) != "" || strings.TrimSpace(req.MelodyID) != "" || (op == "instrumental" && strings.TrimSpace(req.InstrumentalID) != "")) {
		return httpx.BadRequest("当前参考音乐或续写模式无法保证排除说唱，请关闭排除说唱或移除参考音乐")
	}
	lyrics := strings.TrimSpace(req.Prompt)
	style := strings.TrimSpace(req.Tags)
	desc := strings.TrimSpace(req.GPTDescriptionPrompt)
	payload := map[string]interface{}{
		"title":      title,
		"operation":  "generate",
		"created_by": "admin:" + user.Username,
	}
	modelName := strings.TrimSpace(req.MurekaModel)
	if modelName == "" {
		modelName = s.cfg.MurekaModel
	}
	if !validMurekaModel(modelName) {
		return httpx.BadRequest("高级模式模型版本不受支持")
	}
	if op == "instrumental" && modelName == "mureka-o2" {
		return httpx.BadRequest("纯音乐不支持 O2 模型")
	}
	payload["model"] = modelName

	if op == "" && lyrics == "" {
		if desc == "" {
			return httpx.BadRequest("请填写歌词或音乐描述")
		}
		if req.VoiceID == 0 && strings.TrimSpace(req.ReferenceID) == "" && strings.TrimSpace(req.MelodyID) == "" {
			// 纯描述直接走提示词生歌，无需额外先调用一次歌词接口。
			payload["operation"] = "easy"
			style = desc
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
			_, generated, err := s.vocal.GenerateLyrics(ctx, desc)
			cancel()
			if err != nil {
				return httpx.BadRequest("按描述写歌词失败：" + err.Error())
			}
			lyrics = generated
			if style == "" && strings.TrimSpace(req.ReferenceID) == "" && strings.TrimSpace(req.MelodyID) == "" {
				style = desc
			}
		}
	}
	if op == "instrumental" {
		payload["operation"] = "instrumental"
		style = firstNonEmptyString(style, desc)
		if style == "" && strings.TrimSpace(req.InstrumentalID) == "" {
			return httpx.BadRequest("请填写纯音乐描述或参考音乐 ID")
		}
		if strings.TrimSpace(req.InstrumentalID) != "" {
			payload["instrumental_id"] = strings.TrimSpace(req.InstrumentalID)
			style = ""
		}
	} else if op == "extend" || op == "remix" {
		if strings.TrimSpace(req.SourceID) == "" && strings.TrimSpace(req.UploadAudioID) == "" {
			return httpx.BadRequest("请填写原歌曲 ID 或上传音频 ID")
		}
		if req.SourceID != "" && req.UploadAudioID != "" {
			return httpx.BadRequest("原歌曲 ID 与上传音频 ID 只能填写一个")
		}
		if req.SourceID != "" {
			payload["song_id"] = strings.TrimSpace(req.SourceID)
		} else {
			payload["upload_audio_id"] = strings.TrimSpace(req.UploadAudioID)
		}
		payload["operation"] = op
		if op == "extend" {
			if req.ContinueAt == nil || *req.ContinueAt < 0 {
				return httpx.BadRequest("请填写续写起点（秒）")
			}
			if modelName != "mureka-8" && modelName != "mureka-7.6" {
				payload["model"] = "mureka-8"
			}
			payload["extend_at"] = int64(*req.ContinueAt * 1000)
			extendType := strings.TrimSpace(req.ExtendType)
			if extendType != "" && extendType != "head" && extendType != "tail" {
				return httpx.BadRequest("续写方向只能是开头或结尾")
			}
			payload["extend_type"] = firstNonEmptyString(extendType, "tail")
		} else if style == "" {
			return httpx.BadRequest("混音需要填写新风格")
		}
	}
	if op != "instrumental" && payload["operation"] != "easy" {
		if lyrics == "" {
			return httpx.BadRequest("请填写歌词")
		}
		max := murekaLyricsMax
		if op == "extend" {
			max = 3000
		}
		if utf8.RuneCountInString(lyrics) > max {
			return httpx.BadRequest(fmt.Sprintf("歌词最多 %d 个字符", max))
		}
		payload["lyrics"] = lyrics
	}
	if req.ExcludeRap {
		style = strings.TrimSpace(style + ", no rap vocals, no hip hop, no trap, no drill, no spoken word")
		style = strings.TrimPrefix(style, ", ")
	}
	promptMax := murekaPromptMax
	if payload["operation"] == "easy" {
		promptMax = 2000
	}
	if utf8.RuneCountInString(style) > promptMax {
		return httpx.BadRequest(fmt.Sprintf("风格描述最多 %d 个字符", promptMax))
	}
	if style != "" {
		payload["prompt"] = style
	}
	if op == "" {
		if req.VoiceID > 0 {
			vocalID, vocalName, err := s.vocalOf(r, req.VoiceID, merchant.ID)
			if err != nil {
				return err
			}
			payload["vocal_id"] = vocalID
			payload["vocal_name"] = vocalName
			payload["voice_task_id"] = req.VoiceID
		}
		if ref := strings.TrimSpace(req.ReferenceID); ref != "" {
			if style != "" {
				return httpx.BadRequest("音乐参考不能与风格描述同时使用")
			}
			payload["reference_id"] = ref
		}
		if melody := strings.TrimSpace(req.MelodyID); melody != "" {
			if _, ok := payload["reference_id"]; ok || req.VoiceID > 0 || style != "" {
				return httpx.BadRequest("旋律参考不能与风格、音色或音乐参考同时使用")
			}
			payload["melody_id"] = melody
		}
		if gender := strings.TrimSpace(req.Gender); gender != "" {
			if gender != "male" && gender != "female" {
				return httpx.BadRequest("人声倾向只能是男声或女声")
			}
			payload["gender"] = gender
		}
	}
	ids, err := s.tasks.Submit(r.Context(), merchant.ID, model.KindVoiceSong, payload, 0)
	if err != nil {
		return err
	}
	balance, _ := s.store.Balance(r.Context(), merchant.ID)
	httpx.JSON(w, map[string]interface{}{
		"task_ids": ids, "balance": balance, "cost": 0, "lyrics": lyrics,
	})
	return nil
}

func validMurekaModel(v string) bool {
	switch v {
	case "auto", "mureka-7.6", "mureka-o2", "mureka-8", "mureka-9", "mureka-9.5":
		return true
	}
	return false
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// vocalOf 取出演唱音色的 vocal_id，并确认属于该商户。
func (s *Server) vocalOf(r *http.Request, taskID, merchantID int64) (string, string, error) {
	t, raw, err := s.store.AdminTaskByID(r.Context(), taskID)
	if err != nil || t.Task.Kind != model.KindVoiceClone {
		if err == nil || errors.Is(err, storage.ErrNotFound) {
			return "", "", httpx.NotFound("演唱音色不存在")
		}
		return "", "", err
	}
	if t.MerchantID != merchantID {
		return "", "", httpx.BadRequest("该演唱音色不属于所选商户")
	}
	var p struct {
		Name    string `json:"name"`
		VocalID string `json:"vocal_id"`
	}
	if json.Unmarshal([]byte(raw), &p) != nil || p.VocalID == "" {
		return "", "", httpx.Internal("演唱音色数据缺少 vocal_id")
	}
	if s.cfg.MurekaConfigured() && strings.HasPrefix(p.VocalID, "mock-") {
		return "", "", httpx.BadRequest("此音色是未配置服务时创建的模拟音色，请重新上传清唱创建真实演唱音色后再提交")
	}
	return p.VocalID, p.Name, nil
}

// fetchSample 下载清唱音频，最多读 50MB（转码前可能是较大的 WAV）。
func fetchSample(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSampleSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxSampleSize {
		return nil, errors.New("音频为空或超过 50MB")
	}
	return data, nil
}
