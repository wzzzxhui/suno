-- SUNO 开放平台后端表结构（MySQL 5.7+ / 8.0）
-- 使用：mysql -uroot -p < schema.sql

CREATE DATABASE IF NOT EXISTS `suno_open` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `suno_open`;

-- 商户
CREATE TABLE IF NOT EXISTS `merchants` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`       VARCHAR(100)    NOT NULL COMMENT '商户名称',
  `status`     TINYINT         NOT NULL DEFAULT 1 COMMENT '1=正常 0=禁用',
  `points`     BIGINT          NOT NULL DEFAULT 0 COMMENT '积分余额',
  `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='商户';

-- API 密钥：只存哈希，明文仅在创建时返回一次
CREATE TABLE IF NOT EXISTS `api_keys` (
  `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id`  BIGINT UNSIGNED NOT NULL,
  `name`         VARCHAR(100)    NOT NULL DEFAULT 'default',
  `key_prefix`   VARCHAR(16)     NOT NULL COMMENT '前缀，用于后台展示',
  `key_hash`     CHAR(64)        NOT NULL COMMENT 'access_key 的 sha256',
  `status`       TINYINT         NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  `last_used_at` DATETIME        NULL,
  `created_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_key_hash` (`key_hash`),
  KEY `idx_merchant` (`merchant_id`),
  CONSTRAINT `fk_api_keys_merchant` FOREIGN KEY (`merchant_id`) REFERENCES `merchants` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='API 密钥';

-- 异步任务
CREATE TABLE IF NOT EXISTS `tasks` (
  `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id`      BIGINT UNSIGNED NOT NULL,
  `kind`             VARCHAR(32)     NOT NULL COMMENT 'generate/sound/upload/...',
  `status`           VARCHAR(16)     NOT NULL DEFAULT 'pending' COMMENT 'pending/processing/completed/failed',
  `provider_task_id` VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '上游任务或片段 ID',
  `custom_id`        VARCHAR(64)     NULL COMMENT 'Suno 音乐 ID（UUID）',
  `proxy_url`        VARCHAR(1024)   NOT NULL DEFAULT '' COMMENT '平台签名代理地址',
  `file_info`        JSON            NULL COMMENT '音频/封面/视频地址',
  `extend`           LONGTEXT        NULL COMMENT '上游完整数据（JSON 字符串）',
  `extra_param`      VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '任务类型标识',
  `request_payload`  JSON            NULL COMMENT '原始请求参数',
  `error_message`    VARCHAR(500)    NOT NULL DEFAULT '',
  `points_cost`      BIGINT          NOT NULL DEFAULT 0,
  `points_refunded`  TINYINT         NOT NULL DEFAULT 0,
  `retry_count`      INT             NOT NULL DEFAULT 0 COMMENT '失败后免费重试的次数',
  `retried_at`       DATETIME        NULL COMMENT '最近一次重试时间，超时从此刻起算',
  `created_at`       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `finished_at`      DATETIME        NULL,
  PRIMARY KEY (`id`),
  KEY `idx_merchant_created` (`merchant_id`, `created_at`),
  KEY `idx_status` (`status`),
  KEY `idx_custom_id` (`custom_id`),
  CONSTRAINT `fk_tasks_merchant` FOREIGN KEY (`merchant_id`) REFERENCES `merchants` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='音乐异步任务';

-- 积分流水
CREATE TABLE IF NOT EXISTS `point_logs` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id` BIGINT UNSIGNED NOT NULL,
  `type`        TINYINT         NOT NULL COMMENT '1=消耗 2=充值 3=手动调整 4=退还',
  `points`      BIGINT          NOT NULL COMMENT '正数入账，负数出账',
  `balance`     BIGINT          NOT NULL COMMENT '变动后余额',
  `remark`      VARCHAR(255)    NOT NULL DEFAULT '',
  `task_id`     BIGINT UNSIGNED NULL,
  `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_merchant_created` (`merchant_id`, `created_at`),
  CONSTRAINT `fk_logs_merchant` FOREIGN KEY (`merchant_id`) REFERENCES `merchants` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='积分流水';

-- 运营后台管理员
CREATE TABLE IF NOT EXISTS `admin_users` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `username`      VARCHAR(64)     NOT NULL,
  `password_hash` VARCHAR(255)    NOT NULL COMMENT 'pbkdf2$迭代次数$盐$哈希',
  `nickname`      VARCHAR(64)     NOT NULL DEFAULT '',
  `role`          VARCHAR(32)     NOT NULL DEFAULT 'admin' COMMENT 'admin=超级管理员 operator=运营',
  `status`        TINYINT         NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  `last_login_at` DATETIME        NULL,
  `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='运营后台管理员';

-- 长 MV：一首歌拆成多段分镜，逐段生成视频后拼接配乐
CREATE TABLE IF NOT EXISTS `mv_projects` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `merchant_id`     BIGINT UNSIGNED NOT NULL,
  `song_task_id`    BIGINT UNSIGNED NOT NULL COMMENT '作品库中的歌曲任务',
  `title`           VARCHAR(200)    NOT NULL DEFAULT '',
  `mode`            VARCHAR(16)     NOT NULL DEFAULT 'auto' COMMENT 'auto=AI 一键 manual=分镜精修',
  `tier`            VARCHAR(16)     NOT NULL DEFAULT 'premium' COMMENT 'economy=图片动效 standard=图片+视频 premium=全视频',
  `reuse_chorus`    TINYINT         NOT NULL DEFAULT 0 COMMENT '重复的副歌复用首次副歌的镜头',
  `subtitles`       TINYINT         NOT NULL DEFAULT 0 COMMENT '是否叠加歌词字幕',
  `status`          VARCHAR(16)     NOT NULL DEFAULT 'draft' COMMENT 'draft/generating/composing/completed/failed',
  `style_note`      VARCHAR(500)    NOT NULL DEFAULT '' COMMENT '运营补充的画面要求',
  `visual_bible`    TEXT            NULL COMMENT '全片统一的角色与画风设定',
  `look`            TEXT            NULL COMMENT '形象设定 JSON：画风、人物、服装、场景、色调与参考图',
  `writer`          VARCHAR(16)     NOT NULL DEFAULT '' COMMENT '分镜来源：claude/template',
  `ratio`           VARCHAR(16)     NOT NULL DEFAULT '16:9',
  `resolution`      VARCHAR(16)     NOT NULL DEFAULT '720p',
  `use_cover`       TINYINT         NOT NULL DEFAULT 0 COMMENT '首镜是否以封面为参考图',
  `cover_url`       VARCHAR(1024)   NOT NULL DEFAULT '',
  `audio_url`       VARCHAR(1024)   NOT NULL DEFAULT '',
  `duration`        DOUBLE          NOT NULL DEFAULT 0,
  `points_cost`     BIGINT          NOT NULL DEFAULT 0,
  `points_refunded` TINYINT         NOT NULL DEFAULT 0,
  `video_key`       VARCHAR(512)    NOT NULL DEFAULT '' COMMENT '成片在 COS 中的路径',
  `error_message`   VARCHAR(500)    NOT NULL DEFAULT '',
  `created_by`      VARCHAR(64)     NOT NULL DEFAULT '',
  `created_at`      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `started_at`      DATETIME        NULL,
  `finished_at`     DATETIME        NULL,
  PRIMARY KEY (`id`),
  KEY `idx_merchant` (`merchant_id`, `id`),
  KEY `idx_status` (`status`),
  CONSTRAINT `fk_mv_projects_merchant` FOREIGN KEY (`merchant_id`) REFERENCES `merchants` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='长 MV 项目';

CREATE TABLE IF NOT EXISTS `mv_segments` (
  `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `project_id`       BIGINT UNSIGNED NOT NULL,
  `seq`              INT             NOT NULL COMMENT '镜头序号，从 0 开始',
  `kind`             VARCHAR(8)      NOT NULL DEFAULT 'video' COMMENT 'video=AI 视频 image=图片动效 reuse=复用其他镜头',
  `reuse_of`         INT             NULL COMMENT '复用的镜头序号',
  `section`          VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '所在歌曲段落，如 Chorus',
  `start_s`          DOUBLE          NOT NULL,
  `end_s`            DOUBLE          NOT NULL,
  `lyrics`           TEXT            NULL COMMENT '这段时间内的歌词',
  `prompt`           TEXT            NULL COMMENT '视频提示词',
  `keyframe_url`     VARCHAR(1024)   NOT NULL DEFAULT '' COMMENT '视频镜头按参考图画出的首帧',
  `status`           VARCHAR(16)     NOT NULL DEFAULT 'pending' COMMENT 'pending/running/completed/failed',
  `provider_task_id` VARCHAR(128)    NOT NULL DEFAULT '',
  `video_url`        VARCHAR(1024)   NOT NULL DEFAULT '' COMMENT '镜头素材地址：视频或图片',
  `attempts`         INT             NOT NULL DEFAULT 0,
  `error_message`    VARCHAR(500)    NOT NULL DEFAULT '',
  `updated_at`       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_project_seq` (`project_id`, `seq`),
  CONSTRAINT `fk_mv_segments_project` FOREIGN KEY (`project_id`) REFERENCES `mv_projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='长 MV 分镜片段';

-- 创作证明：每首作品一份，首次签发扣费，重复下载免费；删除作品不影响已签发的证明
CREATE TABLE IF NOT EXISTS `certificates` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `cert_no`         VARCHAR(32)     NOT NULL COMMENT '证书编号，公开核验用',
  `merchant_id`     BIGINT UNSIGNED NOT NULL,
  `task_id`         BIGINT UNSIGNED NOT NULL COMMENT '作品任务',
  `suno_id`         VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '作品 custom_id',
  `title`           VARCHAR(200)    NOT NULL DEFAULT '',
  `author`          VARCHAR(100)    NOT NULL DEFAULT '' COMMENT '署名作者',
  `lyrics`          TEXT            NULL,
  `tags`            VARCHAR(1000)   NOT NULL DEFAULT '',
  `model_name`      VARCHAR(64)     NOT NULL DEFAULT '',
  `duration`        DOUBLE          NOT NULL DEFAULT 0,
  `audio_sha256`    CHAR(64)        NOT NULL COMMENT '签发时音频文件的 SHA-256',
  `audio_size`      BIGINT          NOT NULL DEFAULT 0,
  `song_created_at` DATETIME        NOT NULL COMMENT '作品创作完成时间',
  `points_cost`     BIGINT          NOT NULL DEFAULT 0,
  `created_at`      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '签发时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_cert_no` (`cert_no`),
  UNIQUE KEY `uk_task` (`task_id`),
  KEY `idx_merchant` (`merchant_id`, `id`),
  CONSTRAINT `fk_certificates_merchant` FOREIGN KEY (`merchant_id`) REFERENCES `merchants` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='作品创作证明';
