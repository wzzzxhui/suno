# SUNO 开放平台后端（Go）

按 open.suno.cn 文档实现的接口服务：鉴权、异步任务编排与上游对接。
只依赖标准库 + `go-sql-driver/mysql`，兼容 Go 1.18。

## 目录结构

```
cmd/server/          入口，含建表与创建商户命令
internal/config/     环境变量配置（支持 .env）
internal/model/      领域模型、任务类型与定价
internal/storage/    MySQL 持久化与建表 SQL
internal/httpx/      统一响应封装、错误码、轻量路由
internal/middleware/ 鉴权、限流、CORS、日志、panic 兜底
internal/provider/   上游抽象：mock（本地模拟）与 suno（真实接口）
internal/task/       任务编排与轮询 worker
internal/api/        对外 HTTP 接口
```

## 快速开始

```bash
cd backend
cp .env.example .env          # 按需修改 MySQL 连接串

go run ./cmd/server -migrate                      # 建库建表
go run ./cmd/server -create-merchant "测试商户"     # 输出 access_key（仅此一次）
go run ./cmd/server                               # 启动服务，默认 :8080
```

创建商户会打印明文 `access_key`，数据库只保存 sha256 哈希，丢失需重新创建。

## 接口一览

统一鉴权：`Authorization: Bearer <access_key>`，请求与响应均为 JSON。
响应封装：`{ "code": 200, "message": "请求成功", "data": ..., "success": true }`。

| 方法 | 路径 | 说明 | 商户扣积分 |
| --- | --- | --- | --- |
| GET | `/api/v1/points/balance` | 查询积分余额 | 0 |
| GET | `/api/v1/points/logs` | 查询积分流水 | 0 |
| POST | `/api/v1/music/generate` | 生成音乐（含延长、翻唱） | 0 |
| POST | `/api/v1/music/sound` | 生成音效 | 0 |
| GET | `/api/v1/music/task` | 查询单个任务 | 0 |
| GET | `/api/v1/music/tasks` | 批量查询任务 | 0 |
| POST | `/api/v1/music/upload` | 上传参考音频 | 0 |
| POST | `/api/v1/music/whole-song` | 合成整首歌 | 0 |
| POST | `/api/v1/music/aligned-lyrics` | 歌词时间戳对齐 | 0 |
| POST | `/api/v1/music/upsample` | Remaster 升采样 | 0 |
| POST | `/api/v1/music/video` | 生成音乐视频 | 0 |
| POST | `/api/v1/music/crop` | 裁剪音乐 | 0 |
| POST | `/api/v1/music/speed` | 调整播放速度 | 0 |
| POST | `/api/v2/music/download-wav` | 创建 WAV 下载任务 | 0 |
| POST | `/api/v2/music/download-mp3` | 创建 MP3 下载任务 | 0 |
| POST | `/api/v2/music/download-m4a` | 创建 M4A 下载任务 | 0 |
| POST | `/api/v1/music/certificate` | 签发创作证明（同步返回） | 0 |
| GET | `/api/v1/music/certificate/download` | 下载创作证明 PDF | 0 |
| GET | `/api/v1/certificate/verify` | 核验创作证明（免鉴权，按 IP 限流） | 0 |
| GET | `/healthz` | 健康检查（免鉴权） | 0 |

平台服务统一由账号提供上游用量，商户任务不扣积分。生成音乐一次产出两条任务。

错误码与文档一致：400 参数错误、401 鉴权失败、402 积分不足、404 任务不存在、429 限流、500 服务异常。

## 任务生命周期

```
提交请求 → 发往 Provider → 落库(pending)
   → worker 轮询 → processing → completed / failed
```

- `task_id`：本平台任务编号（数字），用于查询状态。
- `custom_id`：上游音乐 ID（UUID），延长、翻唱、裁剪、变速等加工都用它。
- 查询结果同时返回字符串 `status` 与数字 `status_code`（1 排队 / 2 处理中 / 3 完成 / 4 失败），便于兼容旧调用方。
- 超过 `SUNO_TASK_TIMEOUT` 仍未完成的任务会被判失败。

## 创作证明

为已完成的作品签发 PDF 证书，代码在 `internal/certificate/`：

- 签发时取作品的 MP3 文件（与后台「下载 MP3」是同一份）计算 SHA-256，连同标题、署名、歌词、风格、模型、创作完成时间写入 `certificates` 表，
  证书编号形如 `SC20260925-7K3QM9XA`。
- 每首作品只签发一份：签发、重复签发与下载均不扣商户积分。
  音频取不到、字体不可用时会返回失败。
- PDF 每次下载按记录重新绘制（A4、200 DPI 整页图片），内容一致；二维码指向 `CERT_VERIFY_URL?no=编号`。
- 需要含中文的字体：`CERT_FONT_FILE`，留空时用 `MV_FONT_FILE`，再没有就找系统常见中文字体（Noto CJK、微软雅黑等）。
  启动日志会提示字体是否可用。
- 删除作品不影响已签发的证明。运营后台「作品库」可代商户签发与下载。

## Provider 切换

默认 `SUNO_PROVIDER=mock`：不访问外部服务，提交后 `SUNO_MOCK_DELAY` 时间即产出可用的假数据，
适合前端联调与演示。

切换到真实上游：

```bash
SUNO_PROVIDER=suno
SUNO_UPSTREAM_BASE=https://open.suno.cn
SUNO_UPSTREAM_KEY=上游平台的 access_key
```

此时本服务负责鉴权、任务编排与结果归一化，生成能力转发给上游。

## 配置项

见 `.env.example`。真实环境变量优先级高于 `.env` 文件。

## 测试

```bash
go test ./...
```

覆盖上游返回结构归一化、mock 任务生命周期、路由与响应封装。
涉及 MySQL 的读写需要连上数据库后手动验证。
