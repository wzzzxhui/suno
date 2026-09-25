<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createMv,
  deleteMv,
  fetchMerchants,
  fetchMvProject,
  fetchMvProjects,
  fetchSongs,
  quoteMv,
  retryMv,
  rewriteMv,
  saveMvStoryboard,
  startMv
} from '@/api'
import { formatTime, thousands } from '@/utils/format'
import MvLookEditor, { emptyLook, fromLook, toLookPayload } from '@/components/MvLookEditor.vue'

const MAX_CLIP = 12 // Seedance 单段上限（秒），与后端一致

const MV_STATUS = {
  draft: { label: '草稿', type: 'info' },
  generating: { label: '生成中', type: 'warning' },
  composing: { label: '合成中', type: 'warning' },
  completed: { label: '已完成', type: 'success' },
  failed: { label: '已失败', type: 'danger' }
}
const SEG_STATUS = {
  pending: { label: '待生成', type: 'info' },
  running: { label: '生成中', type: 'warning' },
  completed: { label: '已完成', type: 'success' },
  failed: { label: '失败', type: 'danger' }
}
const TIERS = [
  { key: 'economy', title: '经济版', desc: '全部 AI 图片 + 缓慢运镜，像精致的动态相册' },
  { key: 'standard', title: '标准版', desc: '开场与副歌用 AI 视频，其余图片运镜，性价比最高' },
  { key: 'premium', title: '高级版', desc: '每个镜头都是 AI 视频，画面最生动' }
]
const SHOT = {
  video: { label: '视频', type: 'primary' },
  image: { label: '图片', type: 'success' },
  reuse: { label: '复用', type: 'info' }
}
const MODES = [
  { key: 'auto', title: 'AI 一键生成', desc: 'AI 读歌词写好分镜后直接开拍，全程无需操作' },
  { key: 'manual', title: '分镜精修', desc: 'AI 先写分镜草稿，你逐段调整画面后再开拍' }
]

const route = useRoute()

/* ---------------------------------- 商户与作品 ---------------------------------- */

const merchants = ref([])
const merchantId = ref('')
const currentMerchant = computed(() => merchants.value.find((m) => m.id === merchantId.value))

async function loadMerchants() {
  const data = await fetchMerchants({ page: 1, size: 200, status: 1 })
  merchants.value = data.list || []
  const fromQuery = Number(route.query.merchant)
  if (fromQuery && merchants.value.some((m) => m.id === fromQuery)) merchantId.value = fromQuery
  else if (!merchantId.value && merchants.value.length) merchantId.value = merchants.value[0].id
}

const songs = ref([])
const songsLoading = ref(false)

async function searchSongs(keyword = '') {
  if (!merchantId.value) return
  songsLoading.value = true
  try {
    const data = await fetchSongs({ merchant_id: merchantId.value, keyword: keyword || undefined, page: 1, size: 30 })
    songs.value = data.list || []
  } finally {
    songsLoading.value = false
  }
}

const duration = (song) => song?.fileInfo?.duration || 0
const clock = (sec) => `${Math.floor(sec / 60)}:${String(Math.round(sec % 60)).padStart(2, '0')}`

/* ---------------------------------- 新建 ---------------------------------- */

const meta = reactive({ subtitles: false, max_duration: 360, ready: true })
const form = reactive({
  song_task_id: null,
  mode: 'auto',
  tier: 'standard',
  reuse_chorus: true,
  subtitles: false,
  style_note: '',
  ratio: '16:9',
  resolution: '480p',
  use_cover: true
})
const look = ref(emptyLook())
const hasRefs = computed(() => look.value.refs.length > 0)

// 三个档位的报价由后端按实际镜头构成与上游成本计算
const quotes = ref({})
const quoting = ref(false)

async function loadQuotes() {
  quotes.value = {}
  if (!form.song_task_id || !merchantId.value) return
  quoting.value = true
  try {
    quotes.value = await quoteMv({
      merchant_id: merchantId.value,
      song_task_id: form.song_task_id,
      reuse_chorus: form.reuse_chorus,
      resolution: form.resolution,
      with_refs: hasRefs.value
    })
  } finally {
    quoting.value = false
  }
}
watch(() => [form.song_task_id, form.resolution, form.reuse_chorus, hasRefs.value], loadQuotes)
const creating = ref(false)

const selectedSong = computed(() => songs.value.find((s) => s.task_id === form.song_task_id))
const segmentCount = computed(() => Math.max(1, Math.ceil(duration(selectedSong.value) / MAX_CLIP)))
const tooLong = computed(() => duration(selectedSong.value) > meta.max_duration)
const quote = computed(() => quotes.value[form.tier]?.price || 0)
const balanceEnough = computed(() => !currentMerchant.value || currentMerchant.value.points >= quote.value)
const canAfford = (points) => !currentMerchant.value || currentMerchant.value.points >= points

async function create() {
  if (!form.song_task_id) return ElMessage.warning('请选择要制作 MV 的作品')
  if (tooLong.value) return ElMessage.warning(`歌曲超过 ${clock(meta.max_duration)}，暂不支持`)

  if (form.mode === 'auto') {
    await ElMessageBox.confirm(
      `将以「${TIERS.find((t) => t.key === form.tier).title}」为「${selectedSong.value.title}」制作约 ${segmentCount.value} 个镜头的 MV，` +
        `从商户「${currentMerchant.value?.name}」扣除 ${quote.value} 积分。生成失败不退积分，可免费重试。`,
      'AI 一键生成 MV',
      { type: 'info', confirmButtonText: '开始生成' }
    )
  }

  creating.value = true
  try {
    const p = await createMv({ merchant_id: merchantId.value, ...form, look: toLookPayload(look.value) })
    ElMessage.success(form.mode === 'auto' ? `已开始生成，扣除 ${quote.value} 积分` : '分镜草稿已生成，请逐段调整后开拍')
    loadMerchants()
    await loadProjects()
    openDetail(p.id)
  } finally {
    creating.value = false
  }
}

/* ---------------------------------- 列表 ---------------------------------- */

const projects = ref([])
const total = ref(0)
const page = ref(1)
const listLoading = ref(false)
let listTimer = null

async function loadProjects() {
  listLoading.value = true
  try {
    const data = await fetchMvProjects({ merchant_id: merchantId.value || undefined, page: page.value, size: 10 })
    projects.value = data.list || []
    total.value = data.total || 0
    Object.assign(meta, {
      subtitles: data.subtitles,
      max_duration: data.max_duration,
      ready: data.ready
    })
    if (!meta.subtitles) form.subtitles = false
  } finally {
    listLoading.value = false
  }

  const busy = projects.value.some((p) => p.status === 'generating' || p.status === 'composing')
  if (busy && !listTimer) listTimer = setInterval(loadProjects, 8000)
  if (!busy && listTimer) {
    clearInterval(listTimer)
    listTimer = null
  }
}

const progress = (p) => (p.segment_total ? Math.round((p.segment_done / p.segment_total) * 100) : 0)

async function remove(p) {
  await ElMessageBox.confirm(`将删除「${p.title}」及其成片文件，无法恢复。`, '删除 MV', {
    type: 'warning',
    confirmButtonText: '确认删除',
    confirmButtonClass: 'el-button--danger'
  })
  await deleteMv(p.id)
  ElMessage.success('已删除')
  if (detail.value?.id === p.id) drawer.value = false
  loadProjects()
}

/* ---------------------------------- 详情与精修 ---------------------------------- */

const drawer = ref(false)
const detail = ref(null)
const edits = reactive({ bible: '', prompts: {}, note: '', look: emptyLook() })
const dirty = ref(false)
const busy = reactive({ save: false, rewrite: false, start: false })
let detailTimer = null

const isDraft = computed(() => detail.value?.status === 'draft')
const shotCounts = computed(() => {
  const counts = { video: 0, image: 0, reuse: 0 }
  for (const seg of detail.value?.segments || []) counts[seg.kind] = (counts[seg.kind] || 0) + 1
  return counts
})
const tierTitle = (key) => TIERS.find((t) => t.key === key)?.title || key
const detailHasRefs = computed(() => (detail.value?.look?.ref_keys || []).length > 0)
// 形象设定改动后要重写分镜才生效（参考图增减还会影响报价）
const lookChanged = computed(
  () =>
    isDraft.value &&
    JSON.stringify(toLookPayload(edits.look)) !== JSON.stringify(toLookPayload(fromLook(detail.value.look)))
)

// 有参考图的视频镜头先画首帧，再以首帧生成视频
function segStatus(seg) {
  if (seg.status === 'running' && seg.kind === 'video' && detailHasRefs.value && !seg.keyframe_url) {
    return { label: '画首帧中', type: 'warning' }
  }
  return SEG_STATUS[seg.status]
}

async function openDetail(id) {
  drawer.value = true
  await refreshDetail(id, true)
}

async function refreshDetail(id = detail.value?.id, reset = false) {
  if (!id) return
  const p = await fetchMvProject(id)
  detail.value = p
  if (reset || !dirty.value) {
    edits.bible = p.visual_bible
    edits.note = p.style_note
    edits.look = fromLook(p.look)
    edits.prompts = Object.fromEntries((p.segments || []).map((s) => [s.id, s.prompt]))
    dirty.value = false
  }

  const running = p.status === 'generating' || p.status === 'composing'
  if (running && !detailTimer) detailTimer = setInterval(() => refreshDetail(), 5000)
  if (!running && detailTimer) {
    clearInterval(detailTimer)
    detailTimer = null
    loadProjects()
  }
}

watch(drawer, (open) => {
  if (!open && detailTimer) {
    clearInterval(detailTimer)
    detailTimer = null
  }
})

async function save(silent = false) {
  busy.save = true
  try {
    const reused = new Set((detail.value.segments || []).filter((s) => s.kind === 'reuse').map((s) => s.id))
    const segments = Object.entries(edits.prompts)
      .filter(([id]) => !reused.has(Number(id)))
      .map(([id, prompt]) => ({ id: Number(id), prompt }))
    detail.value = await saveMvStoryboard({ id: detail.value.id, visual_bible: edits.bible, segments })
    dirty.value = false
    if (!silent) ElMessage.success('分镜已保存')
  } finally {
    busy.save = false
  }
}

async function rewrite() {
  if (dirty.value) {
    await ElMessageBox.confirm('AI 重写会覆盖你当前修改过的分镜，继续吗？', '重写分镜', { type: 'warning' })
  }
  busy.rewrite = true
  try {
    await rewriteMv({ id: detail.value.id, style_note: edits.note, look: toLookPayload(edits.look) })
    await refreshDetail(detail.value.id, true)
    ElMessage.success('分镜已重写')
  } finally {
    busy.rewrite = false
  }
}

async function start() {
  await ElMessageBox.confirm(
    `将按当前分镜生成 ${shotCounts.value.video} 段视频、${shotCounts.value.image} 张图片并合成 MV，` +
      `扣除 ${detail.value.quote} 积分。生成失败不退积分，可免费重试。`,
    '开始生成',
    { type: 'info', confirmButtonText: '开始生成' }
  )
  busy.start = true
  try {
    if (dirty.value) await save(true)
    const data = await startMv(detail.value.id)
    ElMessage.success(`已开始生成，扣除 ${data.cost} 积分，余额 ${thousands(data.balance)}`)
    loadMerchants()
    loadProjects()
    await refreshDetail(detail.value.id, true)
  } finally {
    busy.start = false
  }
}

// 失败不退积分，重试免费；早期已退过款的 MV 重试时按原报价扣费
const retryCost = () => 0

async function retry(p = detail.value) {
  const cost = retryCost(p)
  await ElMessageBox.confirm(
    `已完成的 ${p.segment_done} 个镜头会保留，只重新生成剩下的镜头并合成。` +
      (cost ? `该 MV 此前已退款，本次按原报价扣除 ${cost} 积分。` : '本次重试免费。'),
    '重试 MV',
    { type: 'info', confirmButtonText: '重试' }
  )
  const data = await retryMv(p.id)
  ElMessage.success(data.cost ? `已重新开始，扣除 ${data.cost} 积分，余额 ${thousands(data.balance)}` : '已重新开始，本次免费')
  loadMerchants()
  loadProjects()
  if (drawer.value && detail.value?.id === p.id) await refreshDetail(p.id, true)
}

/* ---------------------------------- 生命周期 ---------------------------------- */

watch(merchantId, () => {
  form.song_task_id = null
  quotes.value = {}
  // 参考图按商户隔离，换商户后不能沿用
  look.value = { ...look.value, refs: [] }
  page.value = 1
  searchSongs()
  loadProjects()
})

onMounted(async () => {
  await loadMerchants()
  // 从作品库「做长 MV」跳转过来时预选作品
  const songId = Number(route.query.song)
  if (songId) {
    await searchSongs()
    if (songs.value.some((s) => s.task_id === songId)) form.song_task_id = songId
  }
})

onBeforeUnmount(() => {
  if (listTimer) clearInterval(listTimer)
  if (detailTimer) clearInterval(detailTimer)
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>AI MV</h2>
        <p class="desc">按歌词切分镜，Seedance 逐段拍摄，再拼接配上原曲，生成整首歌时长的 MV</p>
      </div>
      <el-button :icon="'Refresh'" @click="loadProjects">刷新</el-button>
    </div>

    <el-alert
      v-if="!meta.ready"
      type="error"
      show-icon
      :closable="false"
      style="margin-bottom: 12px"
      title="未配置 COS 存储，无法保存 MV 成片"
      description="请在后端 .env 中填写 TME_COS_* 配置后重启服务"
    />

    <el-row :gutter="12">
      <!-- 新建 -->
      <el-col :xs="24" :lg="10" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>新建 MV</span></template>

          <el-form label-width="84px">
            <el-form-item label="归属商户" required>
              <el-select v-model="merchantId" filterable placeholder="请选择商户" style="width: 100%">
                <el-option v-for="m in merchants" :key="m.id" :label="m.name" :value="m.id" />
              </el-select>
              <div v-if="currentMerchant" class="text-muted hint">
                当前余额 {{ thousands(currentMerchant.points) }} 积分
                <span v-if="selectedSong && !balanceEnough" style="color: #f56c6c">（不足 {{ quote }}，请先充值）</span>
              </div>
            </el-form-item>

            <el-form-item label="选择作品" required>
              <el-select
                v-model="form.song_task_id"
                filterable
                remote
                :remote-method="searchSongs"
                :loading="songsLoading"
                placeholder="输入歌名搜索该商户的作品"
                style="width: 100%"
              >
                <el-option v-for="s in songs" :key="s.task_id" :value="s.task_id" :label="s.title">
                  <span>{{ s.title }}</span>
                  <span class="mono text-muted" style="float: right">{{ duration(s) ? clock(duration(s)) : '—' }}</span>
                </el-option>
              </el-select>
              <div v-if="selectedSong" class="text-muted hint">
                时长 {{ clock(duration(selectedSong)) }}，约 {{ segmentCount }} 个镜头
                <span v-if="tooLong" style="color: #f56c6c">（超过 {{ clock(meta.max_duration) }} 上限）</span>
              </div>
            </el-form-item>

            <el-form-item label="档位">
              <div class="tiers">
                <div
                  v-for="t in TIERS"
                  :key="t.key"
                  :class="['mode', { active: form.tier === t.key }]"
                  @click="form.tier = t.key"
                >
                  <div class="mode-title">
                    {{ t.title }}
                    <span v-if="quotes[t.key]" class="tier-price">{{ quotes[t.key].price }} 积分</span>
                  </div>
                  <div class="mode-desc">{{ t.desc }}</div>
                  <div v-if="quotes[t.key]" class="mode-desc">
                    视频 {{ quotes[t.key].videos }} · 图片 {{ quotes[t.key].images }} · 复用 {{ quotes[t.key].reused }}
                  </div>
                </div>
              </div>
              <div class="text-muted hint">
                <template v-if="quoting">正在计算报价…</template>
                <template v-else-if="quotes[form.tier]">
                  固定价，按镜头构成计算；上游成本约 {{ quotes[form.tier].cost }} 积分
                </template>
                <template v-else>选择作品后显示各档位报价</template>
              </div>
            </el-form-item>

            <el-form-item label="生成方式">
              <div class="modes">
                <div
                  v-for="m in MODES"
                  :key="m.key"
                  :class="['mode', { active: form.mode === m.key }]"
                  @click="form.mode = m.key"
                >
                  <div class="mode-title">{{ m.title }}</div>
                  <div class="mode-desc">{{ m.desc }}</div>
                </div>
              </div>
            </el-form-item>

            <el-form-item label="形象设定">
              <MvLookEditor
                v-model="look"
                :merchant-id="merchantId"
                :style-note="form.style_note"
                @charged="loadMerchants"
              />
            </el-form-item>

            <el-form-item label="补充要求">
              <el-input
                v-model="form.style_note"
                type="textarea"
                :rows="2"
                maxlength="500"
                show-word-limit
                placeholder="选填，其他想要的画面，如：故事是一段夏天的暗恋，结尾主角独自看海"
              />
            </el-form-item>

            <el-form-item label="画面">
              <el-radio-group v-model="form.ratio" size="small">
                <el-radio-button value="16:9">横屏 16:9</el-radio-button>
                <el-radio-button value="9:16">竖屏 9:16</el-radio-button>
                <el-radio-button value="1:1">方形 1:1</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="清晰度">
              <el-radio-group v-model="form.resolution" size="small">
                <el-radio-button value="480p">480p</el-radio-button>
                <el-radio-button value="720p">720p</el-radio-button>
                <el-radio-button value="1080p">1080p</el-radio-button>
              </el-radio-group>
              <span class="text-muted" style="margin-left: 8px; font-size: 12px">480p 成本约为 720p 的一半</span>
            </el-form-item>
            <el-form-item label="副歌复用">
              <el-switch v-model="form.reuse_chorus" />
              <span class="text-muted" style="margin-left: 8px; font-size: 12px">副歌重复时沿用首次副歌的镜头，少生成三到五成</span>
            </el-form-item>
            <el-form-item label="歌词字幕">
              <el-switch v-model="form.subtitles" :disabled="!meta.subtitles" />
              <span class="text-muted" style="margin-left: 8px; font-size: 12px">
                {{ meta.subtitles ? '在画面底部叠加歌词' : '需在后端配置字幕字体 MV_FONT_FILE' }}
              </span>
            </el-form-item>
            <el-form-item label="首镜封面">
              <el-switch v-model="form.use_cover" />
              <span class="text-muted" style="margin-left: 8px; font-size: 12px">
                {{ hasRefs ? '已设置人物参考图，将以参考图为准' : '第一个镜头以歌曲封面为参考画面' }}
              </span>
            </el-form-item>

            <el-form-item>
              <el-button
                type="primary"
                :loading="creating"
                :disabled="!meta.ready || !form.song_task_id || tooLong || !quote || (form.mode === 'auto' && !balanceEnough)"
                @click="create"
              >
                {{ form.mode === 'auto' ? `一键生成（${quote} 积分）` : '生成分镜草稿（免费）' }}
              </el-button>
              <div class="text-muted hint">
                {{ creating ? '正在按歌词切分镜…' : '固定价，开始前即确定；生成失败不退积分，可免费重试' }}
              </div>
            </el-form-item>
          </el-form>
          <div class="text-muted hint">
            提示：设置参考图后，视频镜头会先按参考图画出首帧再生成视频，每段多一张图的成本，已计入报价
          </div>
        </el-card>
      </el-col>

      <!-- 列表 -->
      <el-col :xs="24" :lg="14" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>我的 MV</span></template>
          <el-table v-loading="listLoading" :data="projects" size="small" style="width: 100%" @row-click="(r) => openDetail(r.id)">
            <el-table-column prop="title" label="作品" min-width="120" show-overflow-tooltip />
            <el-table-column label="档位 / 方式" width="120">
              <template #default="{ row }">{{ tierTitle(row.tier) }} · {{ row.mode === 'auto' ? 'AI 一键' : '精修' }}</template>
            </el-table-column>
            <el-table-column label="状态" width="150">
              <template #default="{ row }">
                <el-tag :type="MV_STATUS[row.status]?.type" size="small">{{ MV_STATUS[row.status]?.label }}</el-tag>
                <span v-if="row.status === 'generating'" class="text-muted" style="margin-left: 6px; font-size: 12px">
                  {{ row.segment_done }}/{{ row.segment_total }}
                </span>
              </template>
            </el-table-column>
            <el-table-column label="创建时间" width="150">
              <template #default="{ row }"><span class="mono">{{ formatTime(row.created_at) }}</span></template>
            </el-table-column>
            <el-table-column label="操作" width="140">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click.stop="openDetail(row.id)">
                  {{ row.status === 'draft' ? '精修' : '查看' }}
                </el-button>
                <el-button v-if="row.status === 'failed'" link type="warning" size="small" @click.stop="retry(row)">
                  重试
                </el-button>
                <el-button
                  v-if="row.status !== 'generating' && row.status !== 'composing'"
                  link
                  type="danger"
                  size="small"
                  @click.stop="remove(row)"
                >
                  删除
                </el-button>
              </template>
            </el-table-column>
            <template #empty><el-empty description="还没有 MV，从左侧选一首歌开始" :image-size="60" /></template>
          </el-table>
          <el-pagination
            v-if="total > 10"
            v-model:current-page="page"
            :total="total"
            :page-size="10"
            layout="prev, pager, next"
            style="margin-top: 12px; justify-content: flex-end"
            @current-change="loadProjects"
          />
        </el-card>
      </el-col>
    </el-row>

    <!-- 详情 / 精修 -->
    <el-drawer v-model="drawer" :title="detail?.title || 'MV'" size="760px" destroy-on-close>
      <template v-if="detail">
        <div class="detail-head">
          <el-tag :type="MV_STATUS[detail.status]?.type">{{ MV_STATUS[detail.status]?.label }}</el-tag>
          <span class="text-muted">
            {{ tierTitle(detail.tier) }} · {{ clock(detail.duration) }} · 视频 {{ shotCounts.video }} / 图片 {{ shotCounts.image }}
            / 复用 {{ shotCounts.reuse }} · {{ detail.ratio }} {{ detail.resolution }}{{ detail.subtitles ? ' · 字幕' : '' }} ·
            {{ detail.writer === 'llm' ? 'AI 撰写分镜' : '模板分镜' }}
          </span>
        </div>

        <el-progress
          v-if="detail.status === 'generating' || detail.status === 'composing'"
          :percentage="detail.status === 'composing' ? 99 : progress(detail)"
          :format="() => (detail.status === 'composing' ? '合成中' : `${detail.segment_done}/${detail.segment_total}`)"
          style="margin: 12px 0"
        />
        <el-alert
          v-if="detail.status === 'failed'"
          type="error"
          :closable="false"
          show-icon
          :title="detail.error_message || '生成失败'"
          description="失败不退积分，可免费重试；已完成的镜头会保留"
          style="margin: 12px 0"
        >
          <el-button size="small" type="warning" style="margin-top: 6px" @click="retry()">
            {{ retryCost(detail) ? `重试（${retryCost(detail)} 积分）` : '免费重试' }}
          </el-button>
        </el-alert>
        <video v-if="detail.video_url" :src="detail.video_url" controls class="final-video" />

        <!-- 形象设定 -->
        <div class="section-title">形象设定</div>
        <MvLookEditor
          v-model="edits.look"
          :merchant-id="detail.merchant_id"
          :style-note="edits.note"
          :disabled="!isDraft"
          @charged="loadMerchants"
        />

        <div v-if="isDraft" class="rewrite">
          <el-input v-model="edits.note" placeholder="补充要求，留空则按原要求重写" maxlength="500" />
          <el-button :type="lookChanged ? 'primary' : 'default'" :loading="busy.rewrite" :icon="'Refresh'" @click="rewrite">
            按新设定重写分镜
          </el-button>
        </div>
        <div v-if="lookChanged" class="text-muted hint">形象设定已修改，重写分镜后生效，报价会随参考图增减重新计算</div>

        <!-- 整体设定 -->
        <div class="section-title">整体设定（全片统一的人物、场景与画风）</div>
        <el-input
          v-model="edits.bible"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 8 }"
          :readonly="!isDraft"
          @input="dirty = true"
        />

        <!-- 分镜 -->
        <div class="section-title">分镜（{{ detail.segments?.length }}）</div>
        <div v-for="seg in detail.segments" :key="seg.id" class="seg">
          <div class="seg-head">
            <span class="seg-no">#{{ seg.seq + 1 }}</span>
            <el-tag :type="SHOT[seg.kind]?.type" size="small" effect="plain">{{ SHOT[seg.kind]?.label }}</el-tag>
            <span class="mono text-muted">{{ clock(seg.start_s) }} – {{ clock(seg.end_s) }}</span>
            <span v-if="seg.section" class="text-muted" style="font-size: 12px">{{ seg.section }}</span>
            <el-tag v-if="!isDraft" :type="segStatus(seg)?.type" size="small" style="margin-left: auto">
              {{ segStatus(seg)?.label }}<template v-if="seg.attempts > 1"> · 第 {{ seg.attempts }} 次</template>
            </el-tag>
          </div>
          <div v-if="seg.lyrics" class="seg-lyrics">{{ seg.lyrics }}</div>
          <div v-else class="seg-lyrics text-muted">（无歌词：前奏 / 间奏 / 尾奏）</div>
          <div v-if="seg.kind === 'reuse'" class="seg-prompt text-muted">
            沿用第 {{ seg.reuse_of + 1 }} 个镜头的画面，不再生成
          </div>
          <el-input
            v-else-if="isDraft"
            v-model="edits.prompts[seg.id]"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 6 }"
            maxlength="800"
            @input="dirty = true"
          />
          <div v-else class="seg-prompt">{{ seg.prompt }}</div>
          <div v-if="seg.error_message && seg.status !== 'completed'" class="seg-error">{{ seg.error_message }}</div>
          <template v-if="seg.video_url">
            <el-image
              v-if="seg.kind === 'image'"
              :src="seg.video_url"
              :preview-src-list="[seg.video_url]"
              preview-teleported
              fit="cover"
              class="seg-video"
            />
            <video v-else :src="seg.video_url" controls preload="none" class="seg-video" />
          </template>
          <el-image
            v-else-if="seg.keyframe_url"
            :src="seg.keyframe_url"
            :preview-src-list="[seg.keyframe_url]"
            preview-teleported
            fit="cover"
            class="seg-video"
            title="首帧，正在以它为起点生成视频"
          />
        </div>
      </template>

      <template v-if="isDraft" #footer>
        <span v-if="dirty" class="text-muted" style="margin-right: auto">有未保存的修改</span>
        <el-button :loading="busy.save" @click="save()">保存分镜</el-button>
        <el-button
          type="primary"
          :loading="busy.start"
          :disabled="!canAfford(detail?.quote) || lookChanged"
          @click="start"
        >
          开始生成（{{ detail?.quote }} 积分）
        </el-button>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.hint {
  font-size: 12px;
  margin-top: 4px;
  width: 100%;
}

.modes,
.tiers {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  width: 100%;
}

.tiers {
  grid-template-columns: 1fr 1fr 1fr;
}

.tier-price {
  display: block;
  font-weight: normal;
  font-size: 12px;
  color: var(--el-color-danger);
}

.mode {
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  padding: 8px 10px;
  cursor: pointer;
  line-height: 1.4;
  transition: all 0.15s;

  &.active {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }

  .mode-title {
    font-weight: 600;
    font-size: 13px;
  }

  .mode-desc {
    font-size: 12px;
    color: #909399;
  }
}

.detail-head {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
}

.final-video {
  width: 100%;
  max-height: 380px;
  background: #000;
  border-radius: 6px;
  margin: 12px 0;
}

.section-title {
  font-weight: 600;
  margin: 16px 0 8px;
}

.rewrite {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.seg {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 10px;

  .seg-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 4px;
  }

  .seg-no {
    font-weight: 600;
  }

  .seg-lyrics {
    font-size: 12px;
    white-space: pre-line;
    color: #606266;
    margin-bottom: 6px;
  }

  .seg-prompt {
    font-size: 13px;
    line-height: 1.6;
  }

  .seg-error {
    color: #f56c6c;
    font-size: 12px;
    margin-top: 4px;
  }

  .seg-video {
    width: 240px;
    margin-top: 6px;
    border-radius: 4px;
    background: #000;
  }
}
</style>
