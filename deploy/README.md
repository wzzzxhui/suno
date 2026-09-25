# 部署说明（Linux 服务器）

把后端、运营后台、开放平台公开站部署到一台 Ubuntu 22.04 / Debian 12 服务器。

```
Nginx (80/443)
 ├─ API_DOMAIN    → Go 后端 127.0.0.1:8080          对外接口
 ├─ ADMIN_DOMAIN  → 静态文件 /opt/suno/admin         运营后台（/admin/api 转发到 8080）
 └─ OPEN_DOMAIN   → Nuxt 127.0.0.1:3000              开放平台公开站（可选）
MySQL 8.0、ffmpeg、中文字体装在同一台机器
```

## 目录内容

| 文件 | 在哪执行 | 作用 |
| --- | --- | --- |
| `build.ps1` | Windows 开发机 | 编译 Linux 后端、构建两个前端，打成 `release/suno-release.tar.gz` |
| `deploy.conf` | — | 部署参数：域名、数据库密码、安装目录、时区 |
| `install.sh` | 服务器 | 首次部署：装依赖、建库、装文件、配 systemd / Nginx / 每日备份（可重复执行） |
| `upgrade.sh` | 服务器 | 发版更新：备份数据库 → 替换程序 → 补表 → 重启 → 健康检查 |
| `rollback.sh` | 服务器 | 回滚到上一次升级前的程序 |
| `backup.sh` | 服务器 | 备份数据库，cron 每天 03:30 自动执行 |
| `suno-cli.sh` | 服务器 | 执行管理命令：建管理员、建商户、TME 凭证检查 |
| `templates/` | — | systemd 与 Nginx 配置模板，install.sh 会替换其中的占位符 |

## 服务器配置

| 场景 | 配置 |
| --- | --- |
| 只提供接口，不用 AI MV | 2 核 4G，40G SSD |
| 开启 AI MV（720p） | 4 核 8G，80G SSD |
| AI MV 出 1080p，或多个 MV 同时合成 | 8 核 16G，100G+ SSD |

ffmpeg 合成 MV 最吃资源，一次 1080p 合成要占 1–3G 内存，编码时 CPU 满载。

## 首次部署

### 1. 改部署参数

编辑 `deploy/deploy.conf`，填好三个域名和 `DB_PASS`。

- 密码只能用字母、数字、下划线，因为它会拼进 MySQL DSN。
- 不需要公开站就把 `ENABLE_OPEN` 改成 `0`。

### 2. 在 Windows 上打包

```powershell
cd F:\LE\Suno
powershell -ExecutionPolicy Bypass -File deploy\build.ps1            # 不要公开站就加 -SkipOpen
scp deploy\release\suno-release.tar.gz root@服务器IP:/root/
```

打包需要本机装有 Go 1.24+ 和 Node 18+，且 `admin`、根目录都已执行过 `npm install`。

### 3. 在服务器上安装

```bash
cd /root && tar -xzf suno-release.tar.gz
sudo bash suno/deploy/install.sh
```

### 4. 填写业务配置

打开 `.env`：

```bash
sudo vi /opt/suno/backend/.env
```

install.sh 已经自动设好以下几项：`SUNO_HTTP_ADDR`、`SUNO_MYSQL_DSN`、`SUNO_ALLOWED_ORIGINS`、`FFMPEG_PATH`、`MV_FONT_FILE`，
部署公开站时还有 `CERT_VERIFY_URL`（创作证明二维码指向的核验页）。你还需要按实际情况填写：

| 配置项 | 说明 |
| --- | --- |
| `SUNO_PROVIDER=suno`、`SUNO_UPSTREAM_KEY` | 默认是 `mock`，只返回假数据，上线前必须改 |
| `TME_*`、`TME_COS_*` | 音色翻唱；作品转存与 MV 成片也要用 COS 存储桶 |
| `MV_LLM_*` | 写 MV 分镜的大模型，不填就用模板分镜 |
| `CERT_PRICE`、`CERT_ISSUER` | 创作证明价格（默认 100 积分）与印在证书上的签发单位 |
| `SUNO_RATE_LIMIT_PER_MINUTE`、`SUNO_SIGNUP_BONUS` | 限流次数与新商户赠送积分 |

改完重启后端：`sudo systemctl restart suno`

### 5. 创建账号

```bash
sudo /opt/suno/deploy/suno-cli.sh -create-admin admin -admin-password '至少8位密码'
sudo /opt/suno/deploy/suno-cli.sh -create-merchant "商户名"      # 打印的 access_key 只显示这一次
sudo /opt/suno/deploy/suno-cli.sh -tme-check                     # 配了 TME 时检查凭证
```

### 6. 配 HTTPS

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d api.xxx.com -d admin.xxx.com -d open.xxx.com
```

certbot 会自动改写 Nginx 配置并定时续期。之后再执行 install.sh，它检测到 certbot 改过的配置就不会覆盖。

## 发版更新

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File deploy\build.ps1
scp deploy\release\suno-release.tar.gz root@服务器IP:/root/
```

```bash
# 服务器
cd /root && rm -rf suno && tar -xzf suno-release.tar.gz
sudo bash suno/deploy/upgrade.sh
```

升级时的行为：

- 读取的是已安装的 `/opt/suno/deploy/deploy.conf`，不是新包里的，所以不用再改新包里的配置。
- `.env` 和数据库数据保持不变。
- 新版本在 `.env.example` 里加了配置项的话，会提示 `.env` 缺了哪些。
- `/healthz` 不通时，执行 `sudo /opt/suno/deploy/rollback.sh` 回滚。

## 日常运维

```bash
systemctl status suno                          # 后端状态
journalctl -u suno -f                          # 后端实时日志
journalctl -u suno --since "1 hour ago"        # 最近一小时
journalctl -u suno-open -f                     # 公开站日志
sudo systemctl restart suno                    # 改 .env 后重启
curl http://127.0.0.1:8080/healthz             # 健康检查

sudo /opt/suno/deploy/backup.sh                # 手动备份
ls -lh /opt/suno/backup/                       # 备份文件
# 从备份恢复（会覆盖当前数据，先停服务）
sudo systemctl stop suno
gunzip -c /opt/suno/backup/suno_open_XXXX.sql.gz | sudo mysql
sudo systemctl start suno
```

## 注意事项

### 安全

- **云安全组只放行 22 / 80 / 443。** 8080、3000、3306 都只监听本机，不要对外开放。
- **`.env` 里有数据库密码和各平台密钥**，权限是 600。不要提交到 git，也不要发到群里。
- **商户 `access_key` 只在创建时打印一次**，数据库里只存哈希。丢了只能重新创建商户。
- `SUNO_ALLOWED_ORIGINS` 不要用 `*`。install.sh 已经把它设成后台和公开站的域名。

### 运行环境

- **时区。** DSN 用的是 `loc=Local`，服务器时区不对会让流水时间和报表差 8 小时。install.sh 会按 `TIMEZONE` 设置时区。
- **工作目录。** 程序从当前目录读取 `.env`。管理命令要通过 `suno-cli.sh` 执行，或者先 `cd /opt/suno/backend`，否则读不到配置，会连到默认数据库。
- **ffmpeg 版本。** 必须带 `libx264` 和 `drawtext`。Ubuntu / Debian 官方源的 ffmpeg 两个都有，install.sh 也会检查。自己编译的或其他来源的 ffmpeg 可能缺。
- **中文字体。** MV 字幕和创作证明都需要中文字体。install.sh 装了 `fonts-noto-cjk`，并自动把 `MV_FONT_FILE` 指向它（证书默认共用）。
  启动日志里出现「证书字体不可用」时，签发创作证明会失败。
- **磁盘。** MV 合成时素材临时下载到 `/tmp`（systemd 开了 PrivateTmp），合成完自动删除。一个项目会临时占用几百 MB，要留足磁盘空间。

### 业务

- **上线前把 `SUNO_PROVIDER` 改成 `suno`。** 默认的 `mock` 不会调用上游，只返回假数据。
- **COS 存储桶和服务器放在同一地域。** 作品转存和 MV 成片都要上传 COS，同地域走内网，更快也省流量。
- **MV 合成没有全局并发上限。** 同一个项目不会重复合成，但不同项目会同时各起一个 ffmpeg。小内存机器上多个 MV 同时合成，可能内存不足、进程被杀。用 `free -h` 和 `journalctl -u suno | grep mv` 观察。规模上来后，建议在代码里加全局并发限制。
- **上游音视频链接大约一天后失效。** 后端启动时和之后每小时会补转存历史作品。服务停机太久，部分作品可能来不及转存。

### 脚本

- 脚本必须是 LF 换行。build.ps1 打包时已经统一转换。如果直接从 Windows 拷 `.sh` 到服务器，出现 `$'\r': command not found` 时，执行 `sed -i 's/\r$//' *.sh`。
- `install.sh` 可以重复执行：不会覆盖已有的 `.env`、数据库数据和 certbot 改过的 Nginx 配置，但会覆盖程序文件和 `deploy.conf`。
- 脚本按 Ubuntu / Debian（apt、systemd）编写。CentOS 等其他发行版请参照脚本内容手动安装。
