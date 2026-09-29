// Command server 启动 SUNO 开放平台后端，并附带建表与商户初始化命令。
//
// 用法：
//
//	server                        启动 HTTP 服务
//	server -migrate               执行建表 SQL
//	server -create-merchant 名称   创建商户并生成一条 access_key
//	server -create-admin 用户名    创建运营后台账号
//	server -tme-check             检查腾讯唱歌克隆凭证是否可用
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lepro/suno-open-api/internal/adminapi"
	"github.com/lepro/suno-open-api/internal/api"
	"github.com/lepro/suno-open-api/internal/archive"
	"github.com/lepro/suno-open-api/internal/certificate"
	"github.com/lepro/suno-open-api/internal/config"
	"github.com/lepro/suno-open-api/internal/middleware"
	"github.com/lepro/suno-open-api/internal/model"
	"github.com/lepro/suno-open-api/internal/mv"
	"github.com/lepro/suno-open-api/internal/provider"
	"github.com/lepro/suno-open-api/internal/storage"
	"github.com/lepro/suno-open-api/internal/task"
	"github.com/lepro/suno-open-api/internal/upstream"
)

func main() {
	var (
		migrate        = flag.Bool("migrate", false, "执行 internal/storage/schema.sql 建表")
		schemaPath     = flag.String("schema", "internal/storage/schema.sql", "建表 SQL 文件路径")
		createMerchant = flag.String("create-merchant", "", "创建商户并生成 access_key")
		grantPoints    = flag.Int64("points", 0, "创建商户时额外赠送的积分，0 表示按配置默认值")
		createAdmin    = flag.String("create-admin", "", "创建运营后台账号")
		adminPassword  = flag.String("admin-password", "", "运营后台账号密码，至少 8 位")
		tmeCheck       = flag.Bool("tme-check", false, "调用唱歌克隆 ListModels 检查凭证")
	)
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[suno] ")

	cfg := config.Load()

	if *migrate {
		runMigrate(cfg, *schemaPath)
		return
	}

	if *tmeCheck {
		runTMECheck(cfg)
		return
	}

	store, err := storage.Open(cfg.DSN, cfg.MaxOpenConns, cfg.MaxIdleConns, cfg.ConnMaxLifetime)
	if err != nil {
		log.Fatalf("数据库不可用：%v", err)
	}
	defer store.Close()

	if *createMerchant != "" {
		runCreateMerchant(store, cfg, *createMerchant, *grantPoints)
		return
	}

	if *createAdmin != "" {
		runCreateAdmin(store, *createAdmin, *adminPassword)
		return
	}

	runServer(cfg, store)
}

// registerExtraModels 把配置里声明的新模型并入模型目录。
// 格式：代号|版本|展示名|说明，多个用逗号分隔。
func registerExtraModels(cfg *config.Config) {
	sources := map[model.ModelUsage]string{
		model.UsageGenerate: cfg.ExtraGenerateModels,
		model.UsageSound:    cfg.ExtraSoundModels,
		model.UsageRemaster: cfg.ExtraRemasterModels,
	}

	for usage, raw := range sources {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var models []model.MusicModel
		for _, item := range strings.Split(raw, ",") {
			fields := strings.Split(strings.TrimSpace(item), "|")
			if len(fields) == 0 || strings.TrimSpace(fields[0]) == "" {
				continue
			}
			m := model.MusicModel{Code: strings.TrimSpace(fields[0])}
			if len(fields) > 1 {
				m.Version = strings.TrimSpace(fields[1])
			}
			if len(fields) > 2 {
				m.Label = strings.TrimSpace(fields[2])
			}
			if len(fields) > 3 {
				m.Note = strings.TrimSpace(fields[3])
			}
			if m.Label == "" {
				m.Label = m.Code
			}
			models = append(models, m)
		}
		if len(models) > 0 {
			model.RegisterModels(usage, models)
			log.Printf("已从配置注册 %d 个 %s 模型", len(models), usage)
		}
	}
}

// runServer 启动 HTTP 服务与轮询 worker，并处理优雅退出。
func runServer(cfg *config.Config, store *storage.Store) {
	registerExtraModels(cfg)

	var p provider.Provider
	if cfg.UseMock() {
		p = provider.NewMock(cfg.MockCDNBase, cfg.MockDelay)
		log.Printf("使用 mock provider（%s 后产出结果）；配置 SUNO_PROVIDER=suno 切换真实上游", cfg.MockDelay)
	} else {
		if cfg.UpstreamKey == "" {
			log.Fatal("SUNO_PROVIDER=suno 时必须配置 SUNO_UPSTREAM_KEY")
		}
		p = provider.NewSuno(cfg.UpstreamBase, cfg.UpstreamKey, cfg.UpstreamTime)
		log.Printf("使用真实上游：%s", cfg.UpstreamBase)
	}

	// mock provider 没有上游账户，reader 为 nil 时监控器自动停用
	var balanceReader upstream.BalanceReader
	if reader, ok := p.(upstream.BalanceReader); ok {
		balanceReader = reader
	}

	// 音色训练与翻唱走腾讯唱歌克隆；未配置时用本地模拟，方便先联调界面
	var voice provider.Provider
	if cfg.VoiceConfigured() {
		voice = newTME(cfg)
		log.Printf("音色翻唱使用腾讯唱歌克隆：%s", cfg.VoiceBase)
	} else {
		voice = provider.NewMock(cfg.MockCDNBase, cfg.MockDelay)
		log.Printf("未配置 TME_* 密钥，音色训练与翻唱使用本地模拟")
	}
	// 演唱音色创作走 Mureka：上传的清唱直接演唱新歌；未配置时用本地模拟，方便先联调界面
	var vocal provider.VocalService = provider.MockVocal{}
	var vocalSongs provider.Provider = provider.NewMock(cfg.MockCDNBase, cfg.MockDelay)
	if cfg.MurekaConfigured() {
		mureka := provider.NewMureka(provider.MurekaConfig{
			Base: cfg.MurekaBase, APIKey: cfg.MurekaAPIKey, Model: cfg.MurekaModel, Timeout: cfg.UpstreamTime,
		})
		vocal, vocalSongs = mureka, mureka
		log.Printf("演唱音色使用 Mureka：%s", cfg.MurekaBase)
	} else {
		log.Printf("未配置 MUREKA_API_KEY，演唱音色与音色演唱创作使用本地模拟")
	}
	model.Price[model.KindVoiceSong] = cfg.MurekaSongPrice
	model.Price[model.KindVoiceClone] = cfg.MurekaClonePrice

	p = provider.NewRouter(p).
		Route(voice, model.KindVoiceTrain, model.KindVoiceCover).
		Route(vocalSongs, model.KindVoiceSong)

	taskSvc := task.New(store, p, cfg)
	// 音色训练、翻唱结束后删除为它上传到 COS 的音频
	taskSvc.SetCleaner(task.NewSampleCleaner(store, &provider.COSSigner{
		SecretID: cfg.VoiceCOSSecretID, SecretKey: cfg.VoiceCOSSecretKey,
	}, cfg.VoiceOutputBase))
	monitor := upstream.NewMonitor(balanceReader, cfg.UpstreamBalanceInterval, cfg.UpstreamLowThreshold)

	// 作品完成后转存音频与封面到 COS，读取作品时统一换成平台副本的签名地址
	archiver := archive.New(store, p, &provider.COSSigner{
		SecretID: cfg.VoiceCOSSecretID, SecretKey: cfg.VoiceCOSSecretKey,
	}, cfg.VoiceOutputBase)
	if archiver != nil {
		storage.SetFileSigner(func(key string) string { return archiver.Sign(key, cfg.ArchiveURLExpire) })
		taskSvc.SetArchiver(archiver.Async)
		log.Printf("作品音频与封面将转存到 %s", cfg.VoiceOutputBase)
	} else {
		log.Printf("未配置 TME_COS_*，作品不转存，上游音频约一天后失效")
	}

	mvSvc := newMV(cfg, store, p)

	// 作品下载统一给 MP3（上游多是 M4A，需转码），创作证明的指纹也按这份文件计算
	mp3 := archive.NewMP3Maker(store, archiver, cfg.FFmpegPath)
	// 音色翻唱的原曲统一转成 MP3 交给腾讯：Suno 的 m4a 是 Opus 编码，唱歌克隆解不了
	taskSvc.SetCoverSource(func(ctx context.Context, t *model.Task) (string, error) {
		return mp3.URL(ctx, t, 24*time.Hour)
	})

	certs := certificate.New(store, certificate.Options{
		MP3:       mp3.Get,
		Price:     cfg.CertPrice,
		Issuer:    cfg.CertIssuer,
		FontFile:  cfg.CertFontFile,
		VerifyURL: cfg.CertVerifyURL,
		MockAudio: cfg.UseMock(),
	})

	if err := certs.CheckFont(); err != nil {
		log.Printf("[certificate] 证书字体不可用（%v），签发创作证明会失败；请配置 CERT_FONT_FILE 或 MV_FONT_FILE", err)
	}
	if cfg.CertVerifyURL == "" {
		log.Printf("[certificate] 未配置 CERT_VERIFY_URL，创作证明上不印核验二维码")
	}

	server := api.NewServer(cfg, store, taskSvc)
	server.SetCertificates(certs)
	admin := adminapi.NewServer(cfg, store, taskSvc, monitor, mvSvc)
	admin.SetArchiver(archiver)
	admin.SetCertificates(certs)
	admin.SetMP3(mp3)
	admin.SetVocal(vocal)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := task.NewWorker(taskSvc, cfg)
	go worker.Run(ctx)
	go monitor.Run(ctx)
	go mv.NewWorker(mvSvc).Run(ctx)
	go archiver.Backfill(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Routes(admin.Register),
		ReadHeaderTimeout: 10 * time.Second,
		// 后台要接收最大 50MB 的音频上传，读取超时放宽
		ReadTimeout:  3 * time.Minute,
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("HTTP 服务监听 %s", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务启动失败：%v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("收到退出信号，正在关闭…")

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭 HTTP 服务出错：%v", err)
	}
	log.Println("已退出")
}

// runMigrate 按分号切分执行建表语句。
func runMigrate(cfg *config.Config, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("读取建表文件失败：%v", err)
	}

	// 建库语句需要连到不指定库的 DSN 上执行
	rootDSN := stripDatabase(cfg.DSN)
	db, err := storage.Open(rootDSN, 4, 2, time.Hour)
	if err != nil {
		log.Fatalf("数据库不可用：%v", err)
	}
	defer db.Close()

	for _, stmt := range splitSQL(string(raw)) {
		if _, err := db.DB().Exec(stmt); err != nil {
			log.Fatalf("执行失败：%v\nSQL: %s", err, truncate(stmt, 200))
		}
	}
	log.Println("建表完成")
}

// runCreateMerchant 创建商户与密钥，明文 access_key 仅此一次输出。
func runCreateMerchant(store *storage.Store, cfg *config.Config, name string, points int64) {
	bonus := cfg.SignupBonus
	if points > 0 {
		bonus = points
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	merchant, err := store.CreateMerchant(ctx, name, bonus)
	if err != nil {
		log.Fatalf("创建商户失败：%v", err)
	}

	accessKey, err := middleware.GenerateAccessKey()
	if err != nil {
		log.Fatalf("生成密钥失败：%v", err)
	}
	prefix := accessKey[:12]
	if _, err := store.CreateAPIKey(ctx, merchant.ID, "default", prefix, middleware.HashKey(accessKey)); err != nil {
		log.Fatalf("保存密钥失败：%v", err)
	}

	fmt.Println("商户创建成功")
	fmt.Printf("  merchant_id : %d\n", merchant.ID)
	fmt.Printf("  name        : %s\n", merchant.Name)
	fmt.Printf("  points      : %d\n", merchant.Points)
	fmt.Printf("  access_key  : %s\n", accessKey)
	fmt.Println("请立即保存 access_key，数据库只存哈希，无法再次查看。")
}

// runCreateAdmin 创建运营后台账号。
func runCreateAdmin(store *storage.Store, username, password string) {
	username = strings.TrimSpace(username)
	if len(password) < 8 {
		log.Fatal("请用 -admin-password 指定至少 8 位的密码")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := store.AdminByUsername(ctx, username); err == nil {
		log.Fatalf("账号 %s 已存在", username)
	}

	hash, err := adminapi.HashPassword(password)
	if err != nil {
		log.Fatalf("生成口令哈希失败：%v", err)
	}
	user, err := store.CreateAdmin(ctx, username, hash, username, "admin")
	if err != nil {
		log.Fatalf("创建后台账号失败：%v", err)
	}

	fmt.Println("运营后台账号创建成功")
	fmt.Printf("  id       : %d\n", user.ID)
	fmt.Printf("  username : %s\n", user.Username)
	fmt.Println("请使用该用户名与刚才设置的密码登录后台。")
}

// stripDatabase 去掉 DSN 中的库名，用于执行 CREATE DATABASE。
func stripDatabase(dsn string) string {
	slash := strings.LastIndex(dsn, "/")
	if slash < 0 {
		return dsn
	}
	tail := dsn[slash+1:]
	if question := strings.Index(tail, "?"); question >= 0 {
		return dsn[:slash+1] + tail[question:]
	}
	return dsn[:slash+1]
}

// splitSQL 按分号切分语句，跳过注释与空行。
func splitSQL(raw string) []string {
	lines := strings.Split(raw, "\n")
	var buf strings.Builder
	var stmts []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			stmts = append(stmts, strings.TrimSpace(buf.String()))
			buf.Reset()
		}
	}
	if rest := strings.TrimSpace(buf.String()); rest != "" {
		stmts = append(stmts, rest)
	}
	return stmts
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// newTME 按配置创建唱歌克隆客户端。
func newTME(cfg *config.Config) *provider.TME {
	return provider.NewTME(provider.TMEConfig{
		Base:             cfg.VoiceBase,
		GatewaySecretID:  cfg.VoiceGatewaySecretID,
		GatewaySecretKey: cfg.VoiceGatewaySecretKey,
		Source:           cfg.VoiceSource,
		SecretID:         cfg.VoiceSecretID,
		SecretKey:        cfg.VoiceSecretKey,
		ContentID:        cfg.VoiceContentID,
		OutputBase:       cfg.VoiceOutputBase,
		OutputDir:        cfg.VoiceOutputDir,
		Timeout:          cfg.UpstreamTime,
		Signer: &provider.COSSigner{
			SecretID:  cfg.VoiceCOSSecretID,
			SecretKey: cfg.VoiceCOSSecretKey,
			Expire:    cfg.VoiceURLExpire,
		},
	})
}

// runTMECheck 调一次只读的 ListModels，确认签名、临时密钥与开通状态都正常。
func runTMECheck(cfg *config.Config) {
	if !cfg.VoiceConfigured() {
		log.Fatal("TME_GATEWAY_*、TME_SECRET_*、TME_CONTENT_ID 未填全")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	names, err := newTME(cfg).ListModels(ctx)
	if err != nil {
		log.Fatalf("唱歌克隆不可用：%v", err)
	}
	fmt.Printf("唱歌克隆凭证可用，已训练模型 %d 个：%v\n", len(names), names)
	fmt.Printf("结果地址前缀：%s（COS 签名：%v）\n", cfg.VoiceOutputBase,
		cfg.VoiceCOSSecretID != "" && cfg.VoiceCOSSecretKey != "")

	// 再验证一次 COS 上传与签名下载，页面上传音频依赖这条链路
	signer := &provider.COSSigner{SecretID: cfg.VoiceCOSSecretID, SecretKey: cfg.VoiceCOSSecretKey, Expire: time.Minute}
	if !signer.Enabled() || cfg.VoiceOutputBase == "" {
		fmt.Println("未配置 TME_COS_*，跳过 COS 上传检查")
		return
	}
	probe := cfg.VoiceOutputBase + "/_healthcheck/probe.txt"
	body := []byte("suno tme-check " + time.Now().Format(time.RFC3339))
	if err := signer.Put(ctx, http.DefaultClient, probe, "text/plain", body); err != nil {
		log.Fatalf("COS 上传失败：%v", err)
	}
	signed, _ := signer.Sign(probe, time.Now())
	resp, err := http.Get(signed)
	if err != nil {
		log.Fatalf("COS 签名下载失败：%v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("COS 签名下载返回 HTTP %d", resp.StatusCode)
	}
	if err := signer.Delete(ctx, http.DefaultClient, probe); err != nil {
		log.Fatalf("COS 删除失败：%v", err)
	}
	fmt.Println("COS 上传、签名下载与删除正常")
}

// newMV 创建长 MV 服务：补建数据表，并检查 ffmpeg、图片能力与字幕字体是否可用。
func newMV(cfg *config.Config, store *storage.Store, p provider.Provider) *mv.Service {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.EnsureMVTables(ctx); err != nil {
		log.Fatalf("创建 MV 数据表失败：%v", err)
	}

	if missing := mv.CheckFFmpeg(cfg.FFmpegPath); len(missing) > 0 {
		log.Printf("ffmpeg 不满足长 MV 合成要求：%s", strings.Join(missing, "；"))
	} else {
		log.Printf("ffmpeg 检查通过（%s）", cfg.FFmpegPath)
	}
	if _, ok := model.CapabilityOf(model.TaskKind(cfg.MVImageKind)); !ok {
		log.Fatalf("MV_IMAGE_KIND=%s 不是已知的图片能力", cfg.MVImageKind)
	}
	// 配置的字体在当前系统不存在时（如把 Windows 路径带到 Linux），自动在常见位置查找
	if font := mv.ResolveFont(cfg.MVFontFile); font != "" {
		if font != cfg.MVFontFile {
			log.Printf("字幕字体使用 %s", font)
		}
		cfg.MVFontFile = font
	} else {
		log.Printf("未找到中文字体，歌词字幕不可用；请安装中文字体或配置 MV_FONT_FILE")
		cfg.MVFontFile = ""
	}

	// 配置了大模型时由它写分镜，失败或未配置时按模板写
	var writer mv.Writer
	if cfg.MVLLMBaseURL != "" && cfg.MVLLMAPIKey != "" && cfg.MVLLMModel != "" {
		writer = &mv.LLMWriter{BaseURL: cfg.MVLLMBaseURL, APIKey: cfg.MVLLMAPIKey, Model: cfg.MVLLMModel,
			Client: &http.Client{Timeout: 4 * time.Minute}}
		log.Printf("MV 分镜由大模型 %s 撰写", cfg.MVLLMModel)
	} else {
		log.Printf("未配置 MV_LLM_*，MV 分镜按模板生成")
	}

	return mv.NewService(store, p, writer, mv.Options{
		Pricing: mv.Pricing{
			VideoPerSecond: map[string]float64{"480p": cfg.MVVideoCost480, "720p": cfg.MVVideoCost720, "1080p": cfg.MVVideoCost1080},
			ImageCost:      map[string]int64{"480p": cfg.MVImageCost1K, "720p": cfg.MVImageCost1K, "1080p": cfg.MVImageCost2K},
			Markup:         cfg.MVMarkup,
			MinPrice:       cfg.MVMinPrice,
		},
		ImageKind:     model.TaskKind(cfg.MVImageKind),
		Font:          cfg.MVFontFile,
		MaxDuration:   cfg.MVMaxDuration.Seconds(),
		SeedanceModel: cfg.MVSeedanceModel,
		Concurrency:   cfg.MVConcurrency,
		Retries:       cfg.MVRetries,
		Timeout:       cfg.MVTimeout,
		FFmpeg:        cfg.FFmpegPath,
		PollInterval:  cfg.PollInterval,
		OutputBase:    cfg.VoiceOutputBase,
		Signer:        &provider.COSSigner{SecretID: cfg.VoiceCOSSecretID, SecretKey: cfg.VoiceCOSSecretKey},
	})
}
