# SUNO API 平台

按 open.suno.cn 的公开文档实现的一套完整平台，三个工程：

```
backend/    Go 服务：对外 19 个业务接口 + 运营后台接口（鉴权、积分、任务编排、上游对接、创作证明）
admin/      安沐心平台（运营后台）前端：Vue 3 + Element Plus + Pinia + ECharts
（根目录）   开放平台公开站：Nuxt 3，API 文档、使用指南与在线测试工具（给接入方看）
```

## 一次跑通

### 1. 后端

需要一个可用的 MySQL。

```bash
cd backend
cp .env.example .env         # 改成你的 MySQL 账号密码

go run ./cmd/server -migrate                              # 建库建表
go run ./cmd/server -create-admin admin -admin-password 你的密码   # 安沐心平台账号
go run ./cmd/server -create-merchant "测试商户"             # 商户 + access_key
go run ./cmd/server                                       # 监听 :8080
```

默认使用 mock provider，不访问任何外部服务，提交任务十几秒后即产出可用的假结果。
要对接真实上游，在 `.env` 里设置 `SUNO_PROVIDER=suno` 与 `SUNO_UPSTREAM_KEY`。

### 2. 安沐心平台（运营后台）

```bash
cd admin
npm install && npm run dev    # http://localhost:5180
```

用上一步创建的管理员账号登录。详见 [admin/README.md](admin/README.md)。

### 3. 开放平台公开站（可选）

```bash
npm install && npm run dev    # http://localhost:3000
```

面向接入方的 API 文档与在线测试工具，商户拿自己的 `access_key` 即可直接调接口。

## 接口能力

19 个业务接口，分三组：

- **系统通用**：查询积分余额、查询积分流水
- **音乐生成**：生成音乐（含延长 / 翻唱）、生成音效、查询任务、批量查询任务、
  上传参考音频、合成整首歌、歌词时间戳、Remaster、生成音乐视频、裁剪、变速，
  以及 WAV / MP3 / M4A 三个格式下载接口
- **创作证明**：签发创作证明（PDF，签发和下载不扣商户积分）、下载证明、公开核验（免鉴权）

参数、定价与响应结构见 [backend/README.md](backend/README.md)。

## 关键概念

- `task_id` 是平台任务编号（数字），只用于查询状态；
  `custom_id` 是音乐 ID（UUID），后续所有加工操作都用它。
- 生成音乐与 Remaster 一次产出两条任务，商户任务不扣积分。
- 失败任务可在运营后台重试，原有任务编号保持不变。
- 音视频链接有效期 1 小时，拿到后请及时转存。
- 创作证明是平台自签证书，登记作品信息与签发时音频文件的 SHA-256 指纹，可在公开站「证书核验」页核验；
  不替代著作权登记。
- 平台服务由统一账号承担上游用量，商户积分仅保留为账户信息。

## 测试

```bash
cd backend && go test ./...   # 后端单元测试
cd admin   && npm run build   # 后台前端构建
npm run build                 # 公开站构建
```


## 音乐创作模式

运营后台的音乐创作和 AI MV 页面提供「普通模式」「高级模式」Tab。高级模式可按描述生成歌曲、自定义歌词生成歌曲、纯音乐、歌曲续写、混音；可以选择自己创建的演唱音色，也可以不指定音色。歌词可单独生成后编辑。高级模式的音频素材支持链接上传或 10MB 内文件直传，返回的文件 ID 会自动填入相应创作字段。

高级工具区覆盖官方开放的歌词、歌曲、分析、转谱、分轨、单轨、局部编辑、配乐、视频、语音、上传、专属模型训练和账单查询接口。需要轮询的操作会返回任务 ID，可在相应的「查询任务」功能中查看结果。大文件上传按「创建上传对象 → 追加文件块 → 完成上传」操作。接口密钥仅保存在后端，浏览器请求固定的后台操作接口。

高级模式需要在 `backend/.env` 配置 `MUREKA_API_KEY`。平台商户任务不扣积分；上游用量和额度由配置该密钥的账户承担。上游返回 429 时，应检查该账户的额度与账单。
