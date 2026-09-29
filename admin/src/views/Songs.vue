<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  deleteSong,
  downloadCertificate,
  downloadSongMp3,
  fetchCertificate,
  fetchMerchantOptions,
  fetchSongs,
  fetchTaskDetail,
  generateVideo,
  issueCertificate
} from '@/api'
import { TASK_STATUS, formatTime, playableUrl, thousands } from '@/utils/format'


const loading = ref(false)
const list = ref([])
const total = ref(0)
const merchants = ref([])
const query = reactive({ merchant_id: '', keyword: '', page: 1, size: 12 })

async function load() {
  loading.value = true
  try {
    const data = await fetchSongs({
      merchant_id: query.merchant_id || undefined,
      keyword: query.keyword || undefined,
      page: query.page,
      size: query.size
    })
    list.value = data.list || []
    total.value = data.total || 0
    syncPolling()
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

function reset() {
  query.merchant_id = ''
  query.keyword = ''
  search()
}

/* ---------------------------------- 歌词 ---------------------------------- */

const lyricsDialog = reactive({ visible: false, loading: false, song: null, text: '' })

function parseJSON(value) {
  if (value && typeof value === 'object') return value
  if (!value || typeof value !== 'string') return null
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

function songLyrics(song, detail) {
  const task = detail?.task || {}
  const request = parseJSON(detail?.request_payload)
  if (request?.make_instrumental === true || request?.operation === 'instrumental') return ''
  const extend = parseJSON(task.extend)
  const clips = Array.isArray(extend) ? extend : extend ? [extend] : []
  const clip = clips.find((item) => item?.id === song.custom_id) || clips[0]
  const generated = clip?.metadata?.prompt || clip?.metadata?.lyrics || clip?.lyrics
  if (typeof generated === 'string' && generated.trim()) return generated.trim()

  if (!request) return ''
  if (typeof request.lyrics === 'string' && request.lyrics.trim()) return request.lyrics.trim()
  if (['generate', 'extend', 'cover'].includes(task.kind) && typeof request.prompt === 'string' && request.prompt.trim()) {
    return request.prompt.trim()
  }
  return ''
}

async function openLyrics(song) {
  Object.assign(lyricsDialog, { visible: true, loading: true, song, text: '' })
  try {
    const detail = await fetchTaskDetail(song.task_id)
    if (lyricsDialog.song?.task_id === song.task_id) {
      lyricsDialog.text = songLyrics(song, detail)
    }
  } catch {
    if (lyricsDialog.song?.task_id === song.task_id) lyricsDialog.visible = false
  } finally {
    if (lyricsDialog.song?.task_id === song.task_id) lyricsDialog.loading = false
  }
}

/* ---------------------------------- MV ---------------------------------- */

const creating = ref(0)

/** MV 的四种状态，决定卡片上显示什么按钮 */
function videoState(song) {
  if (!song.video_task_id) return 'none'
  if (song.video_status === 'completed') return song.video_url ? 'ready' : 'empty'
  if (song.video_status === 'failed') return 'failed'
  return 'running'
}

async function createVideo(song) {
  await ElMessageBox.confirm(
    `将为「${song.title}」生成 MV。`,
    '生成音乐视频',
    { type: 'info' }
  )

  creating.value = song.task_id
  try {
    const data = await generateVideo({ task_id: song.task_id })
    ElMessage.success('已提交生成')
    await load()
  } finally {
    creating.value = 0
  }
}

/* ---------------------------------- 删除 ---------------------------------- */

const deleting = ref(0)

async function removeSong(song) {
  await ElMessageBox.confirm(
    `将永久删除「${song.title}」及其 MV、下载等关联记录，并清空其中保存的音频、封面与视频地址，删除后无法恢复。` +
      '删除后无法恢复。',
    '删除作品',
    { type: 'warning', confirmButtonText: '确认删除', confirmButtonClass: 'el-button--danger' }
  )

  deleting.value = song.task_id
  try {
    const data = await deleteSong(song.task_id)
    ElMessage.success(`已删除 ${data.deleted} 条记录`)
    // 删掉本页最后一条时回到上一页
    if (list.value.length === 1 && query.page > 1) query.page--
    await load()
  } finally {
    deleting.value = 0
  }
}

/* ---------------------------------- 创作证明 ---------------------------------- */

const cert = reactive({
  visible: false,
  loading: false,
  issuing: false,
  song: null,
  info: null, // 接口返回：price、balance、merchant_name、certificate、verify_url
  author: ''
})
const downloading = ref('')

async function openCertificate(song) {
  Object.assign(cert, { visible: true, loading: true, song, info: null, author: '' })
  try {
    cert.info = await fetchCertificate(song.task_id)
    cert.author = cert.info.merchant_name
  } catch {
    cert.visible = false
  } finally {
    cert.loading = false
  }
}

const certIssued = computed(() => cert.info?.certificate)

async function submitCertificate() {
  if (!cert.author.trim()) {
    ElMessage.warning('请填写署名作者')
    return
  }
  cert.issuing = true
  try {
    const data = await issueCertificate({ task_id: cert.song.task_id, author: cert.author.trim() })
    cert.info = { ...cert.info, certificate: data.certificate, verify_url: data.verify_url, balance: data.balance }
    ElMessage.success(
      '创作证明已签发'
    )
    cert.song.certificate_no = data.certificate.certificate_no
    await download(data.certificate.certificate_no)
  } finally {
    cert.issuing = false
  }
}

async function download(no) {
  downloading.value = no
  try {
    await downloadCertificate(no)
  } finally {
    downloading.value = ''
  }
}

/* ---------------------------------- 下载 MP3 ---------------------------------- */

const downloadingMp3 = ref(0)

async function downloadMp3(song) {
  downloadingMp3.value = song.task_id
  try {
    await downloadSongMp3(song.task_id)
  } finally {
    downloadingMp3.value = 0
  }
}

/* ---------------------------------- 轮询 ---------------------------------- */

let timer = null

// 有 MV 正在生成时才轮询，全部出结果就停
function syncPolling() {
  const running = list.value.some((s) => videoState(s) === 'running')
  if (running && !timer) {
    timer = setInterval(load, 5000)
  } else if (!running && timer) {
    clearInterval(timer)
    timer = null
  }
}

/* ---------------------------------- 预览 ---------------------------------- */

const videoVisible = ref(false)
const previewing = ref(null)

function openVideo(song) {
  previewing.value = song
  videoVisible.value = true
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选中复制')
  }
}

const durationText = (song) => {
  const seconds = song.fileInfo?.duration
  if (!seconds) return '—'
  const m = Math.floor(seconds / 60)
  const s = Math.round(seconds % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}

const empty = computed(() => !loading.value && list.value.length === 0)

onMounted(async () => {
  merchants.value = (await fetchMerchantOptions()) || []
  load()
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>作品库</h2>
        <p class="desc">共 {{ total }} 首已完成作品，可查看歌词、试听、下载 MP3、生成 MV 与签发创作证明</p>
      </div>
      <el-button :icon="'Refresh'" @click="load">刷新</el-button>
    </div>

    <el-card shadow="never" body-style="padding:16px">
      <el-form :inline="true" class="filter-bar" @submit.prevent>
        <el-form-item label="商户">
          <el-select v-model="query.merchant_id" placeholder="全部" clearable filterable style="width: 190px">
            <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（${m.id}）`" :value="m.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="关键词">
          <el-input
            v-model="query.keyword"
            placeholder="歌名或 custom_id"
            clearable
            style="width: 220px"
            @keyup.enter="search"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="'Search'" @click="search">查询</el-button>
          <el-button @click="reset">重置</el-button>
        </el-form-item>
      </el-form>

      <el-empty v-if="empty" description="还没有已完成的作品，先去「音乐创作」生成一首" :image-size="90" />

      <div v-else v-loading="loading" class="song-grid">
        <div v-for="song in list" :key="song.task_id" class="song-card">
          <div class="cover">
            <el-image
              v-if="song.fileInfo?.coverUrl"
              :src="song.fileInfo.coverUrl"
              fit="cover"
              :preview-src-list="[song.fileInfo.coverUrl]"
              preview-teleported
              class="cover-img"
            />
            <div v-else class="cover-img cover-empty">
              <el-icon :size="28"><Headset /></el-icon>
            </div>
            <span class="duration">{{ durationText(song) }}</span>
          </div>

          <div class="body">
            <div class="title" :title="song.title">{{ song.title }}</div>
            <div class="meta">
              <el-tag size="small" type="info">{{ song.kind_label }}</el-tag>
              <span class="text-muted">{{ song.merchant_name }}</span>
            </div>

            <audio
              v-if="playableUrl(song)"
              :src="playableUrl(song)"
              controls
              controlslist="nodownload"
              preload="none"
              class="player"
            />

            <div class="id-row mono">
              <span class="id" :title="song.custom_id">{{ song.custom_id }}</span>
              <el-button link type="primary" size="small" @click="copyText(song.custom_id)">复制</el-button>
            </div>

            <div class="song-actions">
              <el-button link type="primary" size="small" @click="openLyrics(song)">查看歌词</el-button>
            </div>

            <div class="cert-row">
              <template v-if="song.certificate_no">
                <el-icon class="cert-ok"><CircleCheckFilled /></el-icon>
                <span class="text-muted">已签发证明</span>
                <el-button
                  link
                  type="primary"
                  size="small"
                  :loading="downloading === song.certificate_no"
                  @click="download(song.certificate_no)"
                >
                  下载
                </el-button>
                <el-button link size="small" @click="openCertificate(song)">详情</el-button>
              </template>
              <el-button v-else link type="primary" size="small" :icon="'Stamp'" @click="openCertificate(song)">
                签发创作证明
              </el-button>
              <el-button
                link
                type="primary"
                size="small"
                class="mp3-btn"
                :icon="'Download'"
                :loading="downloadingMp3 === song.task_id"
                @click="downloadMp3(song)"
              >
                下载 MP3
              </el-button>
            </div>

            <div class="footer">
              <span class="text-muted mono">{{ formatTime(song.created_at) }}</span>

              <template v-if="videoState(song) === 'ready'">
                <el-button size="small" type="success" plain :icon="'VideoPlay'" @click="openVideo(song)">
                  看 MV
                </el-button>
              </template>
              <template v-else-if="videoState(song) === 'running'">
                <el-tag size="small" type="warning">
                  MV {{ TASK_STATUS[song.video_status]?.label || '生成中' }}
                </el-tag>
              </template>
              <template v-else-if="videoState(song) === 'failed'">
                <el-tooltip :content="song.video_message || 'MV 生成失败'" placement="top">
                  <el-button size="small" type="danger" plain @click="createVideo(song)">重试 MV</el-button>
                </el-tooltip>
              </template>
              <template v-else>
                <el-button
                  size="small"
                  :loading="creating === song.task_id"
                  :icon="'VideoCamera'"
                  @click="createVideo(song)"
                >
                  生成 MV
                </el-button>
              </template>
              <el-tooltip content="用 AI 按歌词拍摄整首歌的 MV" placement="top">
                <el-button
                  size="small"
                  type="primary"
                  plain
                  :icon="'VideoCamera'"
                  @click="$router.push({ path: '/mv', query: { song: song.task_id, merchant: song.merchant_id } })"
                >
                  AI MV
                </el-button>
              </el-tooltip>
              <el-button
                size="small"
                type="danger"
                plain
                :icon="'Delete'"
                :loading="deleting === song.task_id"
                @click="removeSong(song)"
              />
            </div>
          </div>
        </div>
      </div>

      <div v-if="!empty" class="pager">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.size"
          :total="total"
          :page-sizes="[12, 24, 48]"
          layout="total, sizes, prev, pager, next"
          @current-change="load"
          @size-change="search"
        />
      </div>
    </el-card>

    <el-dialog v-model="lyricsDialog.visible" :title="'歌词 · ' + (lyricsDialog.song?.title || '')" width="620px" destroy-on-close>
      <div v-loading="lyricsDialog.loading" class="lyrics-dialog">
        <pre v-if="lyricsDialog.text" class="lyrics-text">{{ lyricsDialog.text }}</pre>
        <el-empty v-else-if="!lyricsDialog.loading" description="这首作品暂无可查看的歌词" :image-size="80" />
      </div>
      <template #footer>
        <el-button @click="lyricsDialog.visible = false">关闭</el-button>
        <el-button v-if="lyricsDialog.text" type="primary" @click="copyText(lyricsDialog.text)">复制歌词</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="cert.visible" :title="`创作证明 · ${cert.song?.title || ''}`" width="560px" destroy-on-close>
      <div v-loading="cert.loading" class="cert-dialog">
        <template v-if="certIssued">
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="证书编号">
              <span class="mono">{{ certIssued.certificate_no }}</span>
            </el-descriptions-item>
            <el-descriptions-item label="署名作者">{{ certIssued.author }}</el-descriptions-item>
            <el-descriptions-item label="签发时间">{{ formatTime(certIssued.issued_at) }}</el-descriptions-item>
            <el-descriptions-item label="音频指纹">
              <span class="mono hash">{{ certIssued.audio_sha256 }}</span>
            </el-descriptions-item>
            <el-descriptions-item v-if="cert.info.verify_url" label="核验地址">
              <el-link :href="cert.info.verify_url" target="_blank" type="primary">{{ cert.info.verify_url }}</el-link>
            </el-descriptions-item>
          </el-descriptions>
          <p class="text-muted tip">证明已签发，可重复下载。署名在签发时固定，不能修改。</p>
        </template>

        <template v-else-if="cert.info">
          <el-alert type="info" :closable="false" show-icon>
            <p>
              生成一份 PDF 创作证明，包含作品信息、歌词和签发时音频文件的 SHA-256 指纹，可凭证书编号在线核验。
            </p>
            <p>
              签发后可重复下载。
            </p>
          </el-alert>
          <el-form label-width="80px" class="cert-form" @submit.prevent>
            <el-form-item label="署名作者" required>
              <el-input v-model="cert.author" maxlength="50" show-word-limit placeholder="印在证书上的作者名" />
            </el-form-item>
          </el-form>
          <p class="text-muted tip">署名签发后不能修改。证明用于证明创作时间与来源，不替代著作权登记。</p>
        </template>
      </div>

      <template #footer>
        <el-button @click="cert.visible = false">关闭</el-button>
        <template v-if="certIssued">
          <el-button v-if="cert.info.verify_url" @click="copyText(cert.info.verify_url)">复制核验链接</el-button>
          <el-button
            type="primary"
            :icon="'Download'"
            :loading="downloading === certIssued.certificate_no"
            @click="download(certIssued.certificate_no)"
          >
            下载 PDF
          </el-button>
        </template>
        <div v-else>
          <el-button
            type="primary"
            :icon="'Stamp'"
            :loading="cert.issuing"
            :disabled="cert.loading"
            @click="submitCertificate"
          >
            签发并下载
          </el-button>
        </div>
      </template>
    </el-dialog>

    <el-dialog v-model="videoVisible" :title="previewing?.title || '音乐视频'" width="680px" destroy-on-close>
      <video
        v-if="previewing?.video_url"
        :src="previewing.video_url"
        controls
        autoplay
        style="width: 100%; border-radius: 6px; background: #000"
      ></video>
      <p class="text-muted" style="font-size: 12px; margin-top: 8px">
        链接 1 小时内有效，过期后刷新本页可获取新地址。
      </p>
      <template #footer>
        <el-button @click="copyText(previewing?.video_url)">复制视频地址</el-button>
        <el-button type="primary" @click="videoVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.song-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 14px;
  min-height: 120px;
}

.song-card {
  border: 1px solid #ebeef5;
  border-radius: 8px;
  overflow: hidden;
  background: #fff;
  transition: box-shadow 0.2s;

  &:hover {
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.08);
  }
}

.cover {
  position: relative;
  aspect-ratio: 1 / 1;
  background: #f5f7fa;

  .cover-img {
    width: 100%;
    height: 100%;
    display: block;
  }

  .cover-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    color: #c0c4cc;
  }

  .duration {
    position: absolute;
    right: 6px;
    bottom: 6px;
    background: rgba(0, 0, 0, 0.6);
    color: #fff;
    font-size: 11px;
    padding: 1px 6px;
    border-radius: 3px;
    font-family: 'JetBrains Mono', Consolas, monospace;
  }
}

.body {
  padding: 10px 12px 12px;
}

.title {
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.meta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 6px 0;
  font-size: 12px;
}

.player {
  width: 100%;
  height: 32px;
  margin: 4px 0 6px;
}

.id-row {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;

  .id {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: #909399;
  }
}

.cert-row {
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 24px;
  margin-top: 4px;
  font-size: 12px;

  .cert-ok {
    color: #67c23a;
  }

  .mp3-btn {
    margin-left: auto;
  }
}

.song-actions {
  margin-top: 4px;
}

.lyrics-dialog {
  min-height: 100px;
  max-height: min(65vh, 650px);
  overflow: auto;
}

.lyrics-text {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-family: inherit;
  font-size: 14px;
  line-height: 1.8;
}

.cert-dialog {
  min-height: 120px;

  p {
    margin: 0;
    line-height: 1.7;
  }

  .hash {
    word-break: break-all;
    font-size: 12px;
  }

  .cert-form {
    margin-top: 16px;
  }

  .tip {
    font-size: 12px;
    margin-top: 10px;
  }
}

.footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
  font-size: 11px;
}

// 时间靠左，操作按钮成组靠右
.footer > .text-muted {
  margin-right: auto;
}
</style>
