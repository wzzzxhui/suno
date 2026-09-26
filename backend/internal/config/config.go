package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 汇总服务运行所需的全部配置，全部来自环境变量（支持 .env 文件）。
type Config struct {
	// HTTP
	Addr           string
	AllowedOrigins []string

	// MySQL
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration

	// 上游 Provider：mock | suno
	Provider     string
	UpstreamBase string
	UpstreamKey  string
	UpstreamTime time.Duration

	// 任务轮询
	PollInterval time.Duration
	PollWorkers  int
	TaskTimeout  time.Duration
	// 失败任务可免费重试的次数（失败不退积分）
	TaskMaxRetries int

	// mock provider 产出的资源地址前缀
	MockCDNBase string
	MockDelay   time.Duration

	// 限流：每个商户每分钟允许的请求数
	RateLimitPerMinute int

	// 新商户注册赠送积分
	SignupBonus int64

	// 运营后台：令牌签名密钥与有效期
	AdminSecret   string
	AdminTokenTTL time.Duration

	// 上游余额监控：轮询间隔与低额告警阈值
	UpstreamBalanceInterval time.Duration
	UpstreamLowThreshold    int64

	// 额外模型：上游发新版本时免改代码接入
	// 格式：代号|版本|展示名|说明，多个用逗号分隔
	ExtraGenerateModels string
	ExtraSoundModels    string
	ExtraRemasterModels string

	// 腾讯多媒体实验室「唱歌克隆」：音色训练与音色翻唱
	// 网关密钥用于请求签名，临时密钥放在请求体里，均由 /register 注册后下发
	VoiceBase             string
	VoiceGatewaySecretID  string
	VoiceGatewaySecretKey string
	VoiceSource           string
	VoiceSecretID         string
	VoiceSecretKey        string
	VoiceContentID        string
	// 结果写入 COS 仓库，这里配置仓库根目录对应的可访问地址（COS 公读域名或 CDN）
	VoiceOutputBase   string
	VoiceOutputDir    string
	VoiceTrainTimeout time.Duration
	// 估算训练进度用的每轮耗时（秒）；有训练完成的记录后改用实际平均值
	VoiceTrainSecondsPerEpoch int
	// 注册时绑定的 COS 存储桶；桶为私有读时用 COS 密钥把结果签成临时下载链接
	VoiceCOSBucket    string
	VoiceCOSRegion    string
	VoiceCOSSecretID  string
	VoiceCOSSecretKey string
	VoiceURLExpire    time.Duration
	// 作品转存到 COS 后，对外返回的签名地址有效期（每次读取都会重新签名）
	ArchiveURLExpire time.Duration

	// 长 MV：逐段生成图片或视频后用 ffmpeg 拼接配乐，按档位与时长收固定价
	// 成本（积分）：Seedance 每秒成本按分辨率，图片按张；售价 = 成本 × 加价倍数
	MVVideoCost480  float64
	MVVideoCost720  float64
	MVVideoCost1080 float64
	MVImageCost1K   int64
	MVImageCost2K   int64
	MVMarkup        float64
	MVMinPrice      int64
	MVImageKind     string
	MVFontFile      string
	MVMaxDuration   time.Duration
	MVSeedanceModel string
	MVConcurrency   int
	MVRetries       int
	MVTimeout       time.Duration
	// 写分镜的大模型（兼容 OpenAI Chat Completions），未配置时按模板写
	MVLLMBaseURL string
	MVLLMAPIKey  string
	MVLLMModel   string
	FFmpegPath   string

	// Mureka：用上传的清唱直接生成歌曲（演唱音色）。未配置 API Key 时用本地模拟
	MurekaBase       string
	MurekaAPIKey     string
	MurekaModel      string
	MurekaSongPrice  int64
	MurekaClonePrice int64

	// 创作证明：每份价格（积分，首次签发收取，重复下载免费）、签发单位、证书字体
	// CertVerifyURL 为公开站核验页地址，证书二维码指向「该地址?no=证书编号」，留空则不印二维码
	CertPrice     int64
	CertIssuer    string
	CertFontFile  string
	CertVerifyURL string
}

// Load 读取配置。优先级：真实环境变量 > .env 文件 > 默认值。
func Load() *Config {
	loadDotEnv(".env")

	cfg := &Config{
		Addr:               env("SUNO_HTTP_ADDR", ":8080"),
		AllowedOrigins:     splitAndTrim(env("SUNO_ALLOWED_ORIGINS", "*")),
		DSN:                env("SUNO_MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/suno_open?parseTime=true&charset=utf8mb4&loc=Local"),
		MaxOpenConns:       envInt("SUNO_MYSQL_MAX_OPEN", 32),
		MaxIdleConns:       envInt("SUNO_MYSQL_MAX_IDLE", 8),
		ConnMaxLifetime:    envDuration("SUNO_MYSQL_CONN_LIFETIME", time.Hour),
		Provider:           env("SUNO_PROVIDER", "mock"),
		UpstreamBase:       strings.TrimRight(env("SUNO_UPSTREAM_BASE", "https://open.suno.cn"), "/"),
		UpstreamKey:        env("SUNO_UPSTREAM_KEY", ""),
		UpstreamTime:       envDuration("SUNO_UPSTREAM_TIMEOUT", 60*time.Second),
		PollInterval:       envDuration("SUNO_POLL_INTERVAL", 5*time.Second),
		PollWorkers:        envInt("SUNO_POLL_WORKERS", 4),
		TaskTimeout:        envDuration("SUNO_TASK_TIMEOUT", 15*time.Minute),
		TaskMaxRetries:     envInt("SUNO_TASK_MAX_RETRIES", 3),
		MockCDNBase:        strings.TrimRight(env("SUNO_MOCK_CDN_BASE", "https://cdn.example.com/suno-mock"), "/"),
		MockDelay:          envDuration("SUNO_MOCK_DELAY", 12*time.Second),
		RateLimitPerMinute: envInt("SUNO_RATE_LIMIT_PER_MINUTE", 120),
		SignupBonus:        int64(envInt("SUNO_SIGNUP_BONUS", 60)),
		AdminSecret:        env("SUNO_ADMIN_SECRET", ""),
		AdminTokenTTL:      envDuration("SUNO_ADMIN_TOKEN_TTL", 12*time.Hour),

		UpstreamBalanceInterval: envDuration("SUNO_UPSTREAM_BALANCE_INTERVAL", time.Minute),
		UpstreamLowThreshold:    int64(envInt("SUNO_UPSTREAM_LOW_THRESHOLD", 100)),

		ExtraGenerateModels: env("SUNO_EXTRA_GENERATE_MODELS", ""),
		ExtraSoundModels:    env("SUNO_EXTRA_SOUND_MODELS", ""),
		ExtraRemasterModels: env("SUNO_EXTRA_REMASTER_MODELS", ""),

		VoiceBase:             strings.TrimRight(env("TME_BASE", "https://api.mediax.tencent.com"), "/"),
		VoiceGatewaySecretID:  env("TME_GATEWAY_SECRET_ID", ""),
		VoiceGatewaySecretKey: env("TME_GATEWAY_SECRET_KEY", ""),
		VoiceSource:           env("TME_SOURCE", ""),
		VoiceSecretID:         env("TME_SECRET_ID", ""),
		VoiceSecretKey:        env("TME_SECRET_KEY", ""),
		VoiceContentID:        env("TME_CONTENT_ID", ""),
		VoiceOutputBase:       strings.TrimRight(env("TME_OUTPUT_BASE", ""), "/"),
		VoiceOutputDir:        "/" + strings.Trim(env("TME_OUTPUT_DIR", "/voice_cover"), "/"),
		// 训练一个音色要几十分钟，不能沿用普通任务的超时
		VoiceTrainTimeout: envDuration("TME_TRAIN_TIMEOUT", 3*time.Hour),

		VoiceTrainSecondsPerEpoch: envInt("TME_TRAIN_SECONDS_PER_EPOCH", 20),
		VoiceCOSBucket:            env("TME_COS_BUCKET", ""),
		VoiceCOSRegion:            env("TME_COS_REGION", ""),
		VoiceCOSSecretID:          env("TME_COS_SECRET_ID", ""),
		VoiceCOSSecretKey:         env("TME_COS_SECRET_KEY", ""),
		// 链接写进任务记录后不再刷新，有效期需覆盖作品的使用周期
		VoiceURLExpire:   envDuration("TME_URL_EXPIRE", 30*24*time.Hour),
		ArchiveURLExpire: envDuration("ARCHIVE_URL_EXPIRE", 7*24*time.Hour),

		// 默认值为 2026-09 实测的上游价：Seedance 1.0 pro-fast 每秒 0.037/0.086/0.185 元再打九折，
		// Nano Banana 2 每张 1K 0.20 元、2K 0.25 元
		MVVideoCost480:  envFloat("MV_VIDEO_COST_480P", 3.33),
		MVVideoCost720:  envFloat("MV_VIDEO_COST_720P", 7.74),
		MVVideoCost1080: envFloat("MV_VIDEO_COST_1080P", 16.65),
		MVImageCost1K:   int64(envInt("MV_IMAGE_COST_1K", 20)),
		MVImageCost2K:   int64(envInt("MV_IMAGE_COST_2K", 25)),
		MVMarkup:        envFloat("MV_MARKUP", 2),
		MVMinPrice:      int64(envInt("MV_MIN_PRICE", 100)),
		MVImageKind:     env("MV_IMAGE_KIND", "image_nano2"),
		MVFontFile:      env("MV_FONT_FILE", ""),
		MVMaxDuration:   envDuration("MV_MAX_DURATION", 6*time.Minute),
		MVSeedanceModel: env("MV_SEEDANCE_MODEL", "doubao-seedance-1-0-pro-fast-251015"),
		MVConcurrency:   envInt("MV_CONCURRENCY", 4),
		MVRetries:       envInt("MV_RETRIES", 3),
		MVTimeout:       envDuration("MV_TIMEOUT", 2*time.Hour),
		MVLLMBaseURL:    env("MV_LLM_BASE_URL", ""),
		MVLLMAPIKey:     env("MV_LLM_API_KEY", ""),
		MVLLMModel:      env("MV_LLM_MODEL", ""),
		FFmpegPath:      env("FFMPEG_PATH", "ffmpeg"),

		MurekaBase:       strings.TrimRight(env("MUREKA_BASE", "https://api.mureka.cn"), "/"),
		MurekaAPIKey:     env("MUREKA_API_KEY", ""),
		MurekaModel:      env("MUREKA_MODEL", "auto"),
		MurekaSongPrice:  int64(envInt("MUREKA_SONG_PRICE", 36)),
		MurekaClonePrice: int64(envInt("MUREKA_CLONE_PRICE", 20)),

		CertPrice:     int64(envInt("CERT_PRICE", 100)),
		CertIssuer:    env("CERT_ISSUER", "SUNO API 开放平台"),
		CertFontFile:  env("CERT_FONT_FILE", ""),
		CertVerifyURL: env("CERT_VERIFY_URL", ""),
	}

	// 证书字体默认与 MV 字幕共用
	if cfg.CertFontFile == "" {
		cfg.CertFontFile = cfg.MVFontFile
	}

	// 只配了存储桶时按 COS 默认域名推出结果地址
	if cfg.VoiceOutputBase == "" && cfg.VoiceCOSBucket != "" && cfg.VoiceCOSRegion != "" {
		cfg.VoiceOutputBase = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.VoiceCOSBucket, cfg.VoiceCOSRegion)
	}

	// 未配置密钥时随机生成一个，进程重启后此前签发的登录态失效
	if cfg.AdminSecret == "" {
		cfg.AdminSecret = randomSecret()
	}

	if cfg.PollWorkers < 1 {
		cfg.PollWorkers = 1
	}
	return cfg
}

// MurekaConfigured 返回是否配置了 Mureka，未配置时演唱音色走本地模拟。
func (c *Config) MurekaConfigured() bool { return c.MurekaAPIKey != "" }

// UseMock 返回是否使用本地模拟 Provider。
func (c *Config) UseMock() bool {
	return strings.ToLower(c.Provider) != "suno"
}

// VoiceConfigured 返回唱歌克隆是否已配置；未配置时音色任务走本地模拟。
func (c *Config) VoiceConfigured() bool {
	return c.VoiceGatewaySecretID != "" && c.VoiceGatewaySecretKey != "" &&
		c.VoiceSecretID != "" && c.VoiceSecretKey != "" && c.VoiceContentID != ""
}

// randomSecret 生成一次性的令牌签名密钥。
func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// loadDotEnv 读取 KEY=VALUE 形式的配置文件，不覆盖已存在的环境变量。
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
