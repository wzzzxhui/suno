<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  fetchMerchants,
  fetchModels,
  fetchTaskDetail,
  fetchTasks,
  createVocal,
  deleteVocal,
  fetchVocals,
  generateMusic,
  retryTask,
  uploadMusic
} from '@/api'
import { TASK_STATUS, formatTime, playableUrl, thousands } from '@/utils/format'
import AudioInput from '@/components/AudioInput.vue'

const COST = 36 // 生成音乐单次消耗积分，与后端定价一致

const MODES = [
  { key: 'inspiration', label: '灵感模式', hint: '给一句描述，模型自由发挥' },
  { key: 'custom', label: '自定义歌词', hint: '自己写歌词与风格标签' },
  { key: 'extend', label: '延长', hint: '把已有歌曲从指定秒数续写下去' },
  { key: 'cover', label: '翻唱', hint: '换一套风格重唱已有歌曲' }
]

const mode = ref('inspiration')
const merchants = ref([])
const submitting = ref(false)

// 模型目录由后端下发，上游发新版本时无需改前端
const models = ref([])

// 三组都可自由选择，标签只用于提示新旧程度
const STATUS_GROUPS = [
  { key: 'active', label: '当前版本' },
  { key: 'legacy', label: '历史版本（可正常调用）' },
  { key: 'deprecated', label: '早期版本（上游可能自动转新版）' }
]

const groupedModels = computed(() =>
  STATUS_GROUPS.map((g) => ({
    ...g,
    items: models.value.filter((m) => m.status === g.key)
  })).filter((g) => g.items.length)
)

const form = reactive({
  merchant_id: '',
  title: '夏日回忆',
  mv: 'chirp-hawk',
  make_instrumental: false,
  gpt_description_prompt: '一首欢快的流行歌，关于夏天的美好回忆',
  prompt: '',
  tags: '',
  negative_tags: '',
  continue_clip_id: '',
  continue_at: null,
  cover_clip_id: '',
  vocal_gender: '',
  voice_id: null, // 演唱音色；选了就由 Mureka 直接用这个声音演唱，不选用 Suno
  metadata: ''
})

// Suno 只开放男女声倾向（metadata.vocal_gender）；要用自己的声音，选下方的演唱音色
const VOCAL_OPTIONS = [
  { value: '', label: '自动' },
  { value: 'm', label: '男声' },
  { value: 'f', label: '女声' }
]

// 风格标签预设；上游 tags 是自由文本，下拉仍允许输入预设之外的词
const STYLE_GROUPS = [
  {
    label: '流派',
    items: [
      ['pop', '流行'], ['rock', '摇滚'], ['hip hop', '嘻哈'], ['rap', '说唱'], ['r&b', 'R&B'],
      ['jazz', '爵士'], ['blues', '蓝调'], ['folk', '民谣'], ['country', '乡村'], ['electronic', '电子'],
      ['edm', 'EDM'], ['house', 'House'], ['lo-fi', 'Lo-fi'], ['classical', '古典'], ['metal', '金属'],
      ['punk', '朋克'], ['reggae', '雷鬼'], ['k-pop', 'K-Pop'], ['j-pop', 'J-Pop'], ['c-pop', '华语流行'],
      ['chinese traditional', '中国风'], ['soundtrack', '影视配乐'], ['ambient', '氛围']
    ]
  },
  {
    label: '情绪',
    items: [
      ['happy', '欢快'], ['upbeat', '明快'], ['sad', '忧伤'], ['melancholic', '惆怅'], ['romantic', '浪漫'],
      ['chill', '放松'], ['energetic', '激昂'], ['dreamy', '梦幻'], ['epic', '史诗'], ['dark', '暗黑'],
      ['nostalgic', '怀旧'], ['peaceful', '宁静']
    ]
  },
  {
    label: '乐器',
    items: [
      ['acoustic', '原声'], ['piano', '钢琴'], ['guitar', '吉他'], ['electric guitar', '电吉他'],
      ['bass', '贝斯'], ['drums', '鼓'], ['violin', '小提琴'], ['strings', '弦乐'], ['synth', '合成器'],
      ['saxophone', '萨克斯'], ['guzheng', '古筝'], ['erhu', '二胡'], ['flute', '笛子']
    ]
  },
  {
    label: '人声 / 节奏',
    items: [
      ['female vocals', '女声'], ['male vocals', '男声'], ['duet', '对唱'], ['choir', '合唱'],
      ['slow tempo', '慢节奏'], ['mid tempo', '中速'], ['fast tempo', '快节奏']
    ]
  }
]

const splitTags = (s) => s.split(',').map((t) => t.trim()).filter(Boolean)
const tagsModel = (key) =>
  computed({
    get: () => splitTags(form[key]),
    set: (arr) => (form[key] = arr.map((t) => t.trim()).filter(Boolean).join(', '))
  })
const tagList = tagsModel('tags')
const negativeTagList = tagsModel('negative_tags')

const currentModel = computed(() => models.value.find((m) => m.code === form.mv))

const currentMerchant = computed(() => merchants.value.find((m) => m.id === form.merchant_id))

/* ---------------------------------- 演唱音色 ---------------------------------- */
// 选了演唱音色，歌曲由 Mureka 直接用这个声音演唱：一次生成，不经过 Suno，也不是先生成再翻唱

const vocals = ref([])
const vocalsLoading = ref(false)
const vocalConfigured = ref(true)
const vocalPrices = reactive({ clone: 20, song: 36 })

// 演唱音色属于商户，换商户时重新加载
async function loadVocals(keepSelection = false) {
  if (!keepSelection) form.voice_id = null
  vocals.value = []
  if (!form.merchant_id) return
  vocalsLoading.value = true
  try {
    const data = await fetchVocals({ merchant_id: form.merchant_id })
    vocals.value = data.list || []
    vocalConfigured.value = data.configured
    Object.assign(vocalPrices, data.prices || {})
  } finally {
    vocalsLoading.value = false
  }
}

watch(() => form.merchant_id, () => loadVocals())

// 只有灵感、自定义歌词模式能指定演唱音色，纯音乐没有人声
const voiceAllowed = computed(() => (mode.value === 'inspiration' || mode.value === 'custom') && !form.make_instrumental)
const withVoice = computed(() => voiceAllowed.value && !!form.voice_id)
const voiceName = computed(() => vocals.value.find((v) => v.task_id === form.voice_id)?.name || '')
const totalCost = computed(() => (withVoice.value ? vocalPrices.song : COST))

const vocalDialog = reactive({ visible: false, name: '', audio_url: '', creating: false })

function openVocalDialog() {
  if (!form.merchant_id) return ElMessage.warning('请先选择归属商户')
  Object.assign(vocalDialog, { visible: true, name: '', audio_url: '' })
}

async function submitVocal() {
  if (!vocalDialog.name.trim()) return ElMessage.warning('请填写音色名称')
  if (!/^https?:\/\//.test(vocalDialog.audio_url.trim())) return ElMessage.warning('请上传或录制一段清唱')
  vocalDialog.creating = true
  try {
    const data = await createVocal({
      merchant_id: form.merchant_id,
      name: vocalDialog.name.trim(),
      audio_url: vocalDialog.audio_url.trim()
    })
    ElMessage.success(`演唱音色已创建，扣除 ${data.cost} 积分`)
    await loadVocals(true)
    form.voice_id = data.task_id
    vocalDialog.visible = false
    loadMerchants()
  } finally {
    vocalDialog.creating = false
  }
}

async function removeVocal(v) {
  await ElMessageBox.confirm(`将删除演唱音色「${v.name}」，已扣积分不退还。`, '删除演唱音色', {
    type: 'warning',
    confirmButtonText: '确认删除',
    confirmButtonClass: 'el-button--danger'
  })
  await deleteVocal(v.task_id)
  if (form.voice_id === v.task_id) form.voice_id = null
  ElMessage.success('已删除')
  loadVocals(true)
}

const balanceEnough = computed(() => {
  if (!currentMerchant.value) return true
  return currentMerchant.value.points >= totalCost.value
})

const MV_PREF_KEY = 'suno_admin_preferred_mv'

async function loadModels() {
  const data = await fetchModels()
  models.value = data.generate || []

  // 优先沿用上次选过的模型；它若已不在目录中，再退回服务端默认值
  let preferred = ''
  try {
    preferred = localStorage.getItem(MV_PREF_KEY) || ''
  } catch {
    // 隐私模式下 localStorage 可能不可用
  }

  if (preferred && models.value.some((m) => m.code === preferred)) {
    form.mv = preferred
  } else if (data.defaults?.generate) {
    form.mv = data.defaults.generate
  }
}

// 记住选择，下次打开直接沿用
watch(
  () => form.mv,
  (code) => {
    if (!code) return
    try {
      localStorage.setItem(MV_PREF_KEY, code)
    } catch {
      // 同上
    }
  }
)

async function loadMerchants() {
  const data = await fetchMerchants({ page: 1, size: 200, status: 1 })
  merchants.value = data.list || []
  if (!form.merchant_id && merchants.value.length) {
    form.merchant_id = merchants.value[0].id
  }
}

/* ---------------------------------- 提交 ---------------------------------- */

function buildPayload() {
  const payload = {
    merchant_id: form.merchant_id,
    title: form.title,
    mv: form.mv,
    make_instrumental: form.make_instrumental
  }

  if (mode.value === 'inspiration') {
    payload.gpt_description_prompt = form.gpt_description_prompt
  } else if (mode.value === 'custom') {
    payload.prompt = form.prompt
    payload.tags = form.tags
  } else if (mode.value === 'extend') {
    payload.task = 'extend'
    payload.continue_clip_id = form.continue_clip_id
    if (form.continue_at !== null && form.continue_at !== '') payload.continue_at = Number(form.continue_at)
    payload.prompt = form.prompt
    if (form.tags) payload.tags = form.tags
  } else if (mode.value === 'cover') {
    payload.task = 'cover'
    payload.cover_clip_id = form.cover_clip_id
    payload.prompt = form.prompt
    if (form.tags) payload.tags = form.tags
  }

  if (form.negative_tags) payload.negative_tags = form.negative_tags
  if (form.metadata.trim()) {
    try {
      payload.metadata = JSON.parse(form.metadata)
    } catch {
      throw new Error('高级参数不是合法 JSON')
    }
  }
  // 纯音乐没有人声，不传性别倾向
  if (form.vocal_gender && !form.make_instrumental) {
    payload.metadata = { ...(payload.metadata || {}), vocal_gender: form.vocal_gender }
  }
  if (withVoice.value) payload.voice_id = form.voice_id
  return payload
}

function validate() {
  if (!form.merchant_id) return '请选择归属商户'
  if (!form.title.trim()) return '请填写歌名'
  if (mode.value === 'inspiration' && !form.gpt_description_prompt.trim()) return '请填写音乐描述'
  if (mode.value === 'custom' && !form.prompt.trim()) return '请填写歌词'
  if (mode.value === 'extend' && !form.continue_clip_id.trim()) return '延长模式需要填写原歌曲的 custom_id'
  if (mode.value === 'cover' && !form.cover_clip_id.trim()) return '翻唱模式需要填写原歌曲的 custom_id'
  // 上游要求延长、翻唱也必须带歌词
  if (mode.value === 'extend' && !form.prompt.trim()) return '请填写续写部分的歌词'
  if (mode.value === 'cover' && !form.prompt.trim()) return '请填写翻唱使用的歌词'
  return ''
}

async function submit() {
  const error = validate()
  if (error) {
    ElMessage.warning(error)
    return
  }

  let payload
  try {
    payload = buildPayload()
  } catch (e) {
    ElMessage.warning(e.message)
    return
  }

  submitting.value = true
  try {
    const data = await generateMusic(payload)
    ElMessage.success(
      `已提交，扣除 ${data.cost} 积分，余额 ${thousands(data.balance)}` +
        (withVoice.value ? `；由「${voiceName.value}」直接演唱` : '')
    )
    startTracking(data.task_ids, withVoice.value ? voiceName.value : '')
    loadMerchants()
    loadHistory()
  } finally {
    submitting.value = false
  }
}

/* ---------------------------------- 轮询 ---------------------------------- */

const tracking = ref([])
let timer = null

function startTracking(ids, voice = '') {
  tracking.value = ids.map((id) => ({
    id,
    status: 'pending',
    elapsed: 0,
    task: null,
    voice, // 选了演唱音色时为音色名
    startedAt: Date.now()
  }))
  ensureTimer()
}

const running = (item) => item.status === 'pending' || item.status === 'processing'

// 进度按已用时长估算：Suno 一般 1 分钟左右，Mureka 演唱音色一般 2 分钟左右；完成前最多 95%
function progressOf(item) {
  const expect = item.voice ? 120 : 60
  return Math.min(95, Math.max(2, Math.round(((clock.value - item.startedAt) / 1000 / expect) * 100)))
}

function elapsedText(item) {
  const sec = Math.round((clock.value - item.startedAt) / 1000)
  return sec < 60 ? `${sec} 秒` : `${Math.floor(sec / 60)} 分 ${String(sec % 60).padStart(2, '0')} 秒`
}

// 每秒走一次，进度条和计时连续变化
const clock = ref(Date.now())
const clockTimer = setInterval(() => (clock.value = Date.now()), 1000)

// 生成失败不退积分，可用原参数免费重试，任务编号不变
async function retryItem(item) {
  item.retrying = true
  try {
    await retryTask(item.id)
    Object.assign(item, { status: 'pending', elapsed: 0, task: null, startedAt: Date.now() })
    ElMessage.success('已重新提交')
    ensureTimer()
  } finally {
    item.retrying = false
  }
}

function ensureTimer() {
  if (timer) return
  timer = setInterval(tick, 3000)
  tick()
}

async function tick() {
  const pending = tracking.value.filter(running)
  if (!pending.length) {
    clearInterval(timer)
    timer = null
    return
  }

  await Promise.all(
    pending.map(async (item) => {
      try {
        const data = await fetchTaskDetail(item.id)
        item.task = data.task
        item.status = data.task.status
        item.elapsed += 3
      } catch {
        // 单次查询失败不中断轮询，下个周期再试
      }
    })
  )

  if (tracking.value.some((t) => t.status === 'completed')) loadHistory()
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选中复制')
  }
}

/** 拿完成的歌继续做延长或翻唱 */
// 从任务的 extend（上游原始 JSON）里取出这首歌的歌词
function lyricsOf(task) {
  try {
    const clips = JSON.parse(task?.extend || '[]')
    const clip = (Array.isArray(clips) ? clips : [clips]).find((c) => c.id === task.custom_id) || clips[0]
    return clip?.metadata?.prompt || ''
  } catch {
    return ''
  }
}

function useAsSource(task, target) {
  const customId = task.custom_id
  mode.value = target
  if (target === 'extend') {
    form.continue_clip_id = customId
    form.prompt = ''
  } else {
    form.cover_clip_id = customId
    form.prompt = lyricsOf(task)
  }
  ElMessage.success(`已填入${target === 'extend' ? '延长' : '翻唱'}模式`)
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

/* -------------------------------- 上传原曲 -------------------------------- */

const UPLOAD_COST = 1 // 与后端 KindUpload 定价一致

const coverSource = ref('id') // id：填已有 custom_id；upload：上传音频拿 custom_id
const upload = reactive({
  audio_url: '',
  copyright_audio: false,
  taskId: null,
  status: '',
  error: '',
  elapsed: 0
})
let uploadTimer = null

const uploading = computed(() => upload.status === 'pending' || upload.status === 'processing')

function stopUploadTimer() {
  if (uploadTimer) clearInterval(uploadTimer)
  uploadTimer = null
}

async function submitUpload() {
  if (!form.merchant_id) return ElMessage.warning('请选择归属商户')
  if (!/^https?:\/\//.test(upload.audio_url.trim())) return ElMessage.warning('请填写可公开访问的 http/https 音频链接')

  upload.status = 'pending'
  upload.error = ''
  upload.elapsed = 0
  try {
    const data = await uploadMusic({
      merchant_id: form.merchant_id,
      audio_url: upload.audio_url.trim(),
      copyright_audio: upload.copyright_audio
    })
    upload.taskId = data.task_id
    ElMessage.success(`已提交上传，扣除 ${data.cost} 积分`)
    loadMerchants()
  } catch {
    upload.status = ''
    return
  }

  stopUploadTimer()
  uploadTimer = setInterval(pollUpload, 3000)
  pollUpload()
}

async function pollUpload() {
  try {
    const { task } = await fetchTaskDetail(upload.taskId)
    upload.status = task.status
    upload.elapsed += 3
    if (task.status === 'completed') {
      stopUploadTimer()
      form.cover_clip_id = task.custom_id || ''
      // 上游识别出歌词时顺带填上，识别不到就留给运营手填
      if (!form.prompt.trim()) form.prompt = lyricsOf(task)
      if (form.cover_clip_id) ElMessage.success('上传完成，已填入原歌曲 ID')
      else upload.error = '上传完成但未返回 custom_id'
    } else if (task.status === 'failed') {
      stopUploadTimer()
      upload.error = task.error_message || '上传失败'
    }
  } catch {
    // 单次查询失败不中断轮询
  }
}

/* ---------------------------------- 历史 ---------------------------------- */

const history = ref([])
const historyLoading = ref(false)

async function loadHistory() {
  historyLoading.value = true
  try {
    const data = await fetchTasks({ kind: 'generate', page: 1, size: 10 })
    history.value = data.list || []
  } finally {
    historyLoading.value = false
  }
}

onMounted(() => {
  loadModels()
  loadMerchants()
  loadHistory()
})

onBeforeUnmount(() => {
  clearInterval(clockTimer)
  if (timer) clearInterval(timer)
  stopUploadTimer()
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>音乐创作</h2>
        <p class="desc">在后台代商户发起生成，积分从所选商户账户扣除，一次产出两个版本；选了演唱音色，歌曲直接用你的声音演唱</p>
      </div>
      <el-button :icon="'Refresh'" @click="loadHistory">刷新历史</el-button>
    </div>

    <el-row :gutter="12">
      <!-- 创作表单 -->
      <el-col :xs="24" :lg="14" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>创作参数</span></template>

          <el-form :model="form" label-width="96px">
            <el-form-item label="归属商户" required>
              <el-select v-model="form.merchant_id" filterable placeholder="请选择商户" style="width: 100%">
                <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（余额 ${m.points}）`" :value="m.id" />
              </el-select>
              <div v-if="currentMerchant" class="text-muted" style="font-size: 12px; margin-top: 4px">
                本次消耗 {{ totalCost }} 积分<template v-if="withVoice">（「{{ voiceName }}」演唱）</template>，当前余额
                {{ thousands(currentMerchant.points) }}
                <span v-if="!balanceEnough" style="color: #f56c6c">（余额不足，请先充值）</span>
              </div>
            </el-form-item>

            <el-form-item label="创作模式">
              <el-radio-group v-model="mode">
                <el-radio-button v-for="m in MODES" :key="m.key" :value="m.key">{{ m.label }}</el-radio-button>
              </el-radio-group>
              <div class="text-muted" style="font-size: 12px; margin-top: 4px">
                {{ MODES.find((m) => m.key === mode)?.hint }}
              </div>
            </el-form-item>

            <!-- 灵感模式 -->
            <el-form-item v-if="mode === 'inspiration'" label="音乐描述" required>
              <el-input
                v-model="form.gpt_description_prompt"
                type="textarea"
                :rows="3"
                placeholder="例如：一首欢快的流行歌，关于夏天的美好回忆"
              />
            </el-form-item>

            <!-- 自定义歌词 -->
            <template v-if="mode === 'custom'">
              <el-form-item label="歌词" required>
                <el-input
                  v-model="form.prompt"
                  type="textarea"
                  :rows="8"
                  placeholder="可用 [Verse] [Chorus] 等段落标记分段"
                />
              </el-form-item>
              <el-form-item label="风格标签">
                <el-select v-model="tagList" multiple filterable allow-create default-first-option clearable
                  placeholder="选择或输入风格，回车添加自定义词" style="width: 100%">
                  <el-option-group v-for="g in STYLE_GROUPS" :key="g.label" :label="g.label">
                    <el-option v-for="[v, zh] in g.items" :key="v" :label="v" :value="v">
                      <span>{{ zh }}</span>
                      <span class="mono text-muted" style="float: right">{{ v }}</span>
                    </el-option>
                  </el-option-group>
                </el-select>
              </el-form-item>
            </template>

            <!-- 延长 -->
            <template v-if="mode === 'extend'">
              <el-form-item label="原歌曲 ID" required>
                <el-input v-model="form.continue_clip_id" placeholder="custom_id（UUID），不是数字 task_id" />
              </el-form-item>
              <el-form-item label="从第几秒">
                <el-input-number v-model="form.continue_at" :min="0" :step="5" placeholder="留空则自动" />
                <span class="text-muted" style="margin-left: 8px; font-size: 12px">秒</span>
              </el-form-item>
              <el-form-item label="续写歌词" required>
                <el-input
                  v-model="form.prompt"
                  type="textarea"
                  :rows="6"
                  placeholder="接在原曲后面的新歌词，如 [Verse 3] … [Chorus] …；纯音乐可填 [Instrumental]"
                />
              </el-form-item>
              <el-form-item label="风格标签">
                <el-select v-model="tagList" multiple filterable allow-create default-first-option clearable
                  placeholder="选填，可选择或输入" style="width: 100%">
                  <el-option-group v-for="g in STYLE_GROUPS" :key="g.label" :label="g.label">
                    <el-option v-for="[v, zh] in g.items" :key="v" :label="v" :value="v">
                      <span>{{ zh }}</span>
                      <span class="mono text-muted" style="float: right">{{ v }}</span>
                    </el-option>
                  </el-option-group>
                </el-select>
              </el-form-item>
            </template>

            <!-- 翻唱 -->
            <template v-if="mode === 'cover'">
              <el-form-item label="原曲来源">
                <el-radio-group v-model="coverSource">
                  <el-radio-button value="id">已有歌曲</el-radio-button>
                  <el-radio-button value="upload">上传音频</el-radio-button>
                </el-radio-group>
              </el-form-item>
              <el-form-item v-if="coverSource === 'upload'" label="音频链接" required>
                <el-input v-model="upload.audio_url" placeholder="https://example.com/song.mp3" :disabled="uploading" />
                <div class="upload-row">
                  <el-checkbox v-model="upload.copyright_audio" :disabled="uploading">
                    受版权保护的音频（额外 15 积分）
                  </el-checkbox>
                  <el-button type="primary" plain size="small" :loading="uploading" @click="submitUpload">
                    上传（{{ UPLOAD_COST + (upload.copyright_audio ? 15 : 0) }} 积分）
                  </el-button>
                </div>
                <div class="text-muted" style="font-size: 12px">
                  <template v-if="uploading">上传处理中，已等待 {{ upload.elapsed }} 秒…</template>
                  <span v-else-if="upload.error" style="color: #f56c6c">{{ upload.error }}</span>
                  <template v-else-if="upload.status === 'completed'">上传完成，已自动填入下方原歌曲 ID</template>
                  <template v-else>链接需能被上游直接下载；上传的是要翻唱的原曲，不是音色样本</template>
                </div>
              </el-form-item>
              <el-form-item label="原歌曲 ID" required>
                <el-input v-model="form.cover_clip_id" placeholder="custom_id（UUID），不是数字 task_id" />
              </el-form-item>
              <el-form-item label="歌词" required>
                <el-input
                  v-model="form.prompt"
                  type="textarea"
                  :rows="6"
                  placeholder="翻唱使用的歌词，一般沿用原曲歌词；从本页结果点「用它翻唱」会自动带入"
                />
              </el-form-item>
              <el-form-item label="新风格" required>
                <el-select v-model="tagList" multiple filterable allow-create default-first-option clearable
                  placeholder="选择或输入新风格，如 jazz、piano" style="width: 100%">
                  <el-option-group v-for="g in STYLE_GROUPS" :key="g.label" :label="g.label">
                    <el-option v-for="[v, zh] in g.items" :key="v" :label="v" :value="v">
                      <span>{{ zh }}</span>
                      <span class="mono text-muted" style="float: right">{{ v }}</span>
                    </el-option>
                  </el-option-group>
                </el-select>
              </el-form-item>
            </template>

            <el-form-item label="歌名" required>
              <el-input v-model="form.title" maxlength="100" show-word-limit />
            </el-form-item>

            <el-form-item v-if="!withVoice" label="模型版本">
              <el-select v-model="form.mv" filterable style="width: 100%">
                <el-option-group v-for="g in groupedModels" :key="g.key" :label="g.label">
                  <el-option v-for="m in g.items" :key="m.code" :label="`${m.label}（${m.code}）`" :value="m.code">
                    <span>{{ m.label }}</span>
                    <span class="mono text-muted" style="float: right">{{ m.code }}</span>
                  </el-option>
                </el-option-group>
              </el-select>
              <div class="text-muted" style="font-size: 12px; margin-top: 4px">
                共 {{ models.length }} 个模型可选，选择会被记住；{{ currentModel?.note || '按需挑选任一版本即可' }}
              </div>
            </el-form-item>

            <el-form-item label="纯音乐">
              <el-switch v-model="form.make_instrumental" />
              <span class="text-muted" style="margin-left: 8px; font-size: 12px">开启后不含人声</span>
            </el-form-item>

            <el-form-item v-if="!form.make_instrumental && !withVoice" label="人声">
              <el-radio-group v-model="form.vocal_gender">
                <el-radio-button v-for="o in VOCAL_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</el-radio-button>
              </el-radio-group>
              <div class="text-muted" style="font-size: 12px; margin-top: 4px; width: 100%">
                Suno 演唱时的男女声倾向
              </div>
            </el-form-item>

            <el-form-item v-if="voiceAllowed" label="演唱音色">
              <div class="vocal-row">
                <el-select
                  v-model="form.voice_id"
                  :loading="vocalsLoading"
                  clearable
                  placeholder="不指定，由 Suno 演唱"
                  style="flex: 1"
                  @clear="form.voice_id = null"
                >
                  <el-option v-for="v in vocals" :key="v.task_id" :value="v.task_id" :label="v.name">
                    <span>{{ v.name }}</span>
                    <el-button
                      link
                      type="danger"
                      size="small"
                      style="float: right; margin-top: 6px"
                      @click.stop="removeVocal(v)"
                    >
                      删除
                    </el-button>
                  </el-option>
                </el-select>
                <el-button :icon="'Plus'" @click="openVocalDialog">新建演唱音色</el-button>
              </div>
              <div class="text-muted" style="font-size: 12px; margin-top: 4px; width: 100%">
                <template v-if="withVoice">
                  由「{{ voiceName }}」直接演唱，一次生成两个版本，共 {{ vocalPrices.song }} 积分；伴奏风格按上方描述或风格标签
                </template>
                <template v-else-if="!vocalsLoading && !vocals.length">
                  上传一段 15~30 秒的清唱即可创建自己的演唱音色，之后创作的歌直接用你的声音演唱
                </template>
                <template v-else>选择后歌曲直接用该声音演唱，不再由 Suno 演唱</template>
                <span v-if="!vocalConfigured" style="color: #e6a23c">（未配置 MUREKA_API_KEY，当前为模拟结果）</span>
              </div>
            </el-form-item>

            <el-collapse>
              <el-collapse-item title="高级参数（选填）" name="advanced">
                <el-form-item label="排除风格">
                  <el-select v-model="negativeTagList" multiple filterable allow-create default-first-option clearable
                  placeholder="不希望出现的风格，如 metal" style="width: 100%">
                  <el-option-group v-for="g in STYLE_GROUPS" :key="g.label" :label="g.label">
                    <el-option v-for="[v, zh] in g.items" :key="v" :label="v" :value="v">
                      <span>{{ zh }}</span>
                      <span class="mono text-muted" style="float: right">{{ v }}</span>
                    </el-option>
                  </el-option-group>
                </el-select>
                </el-form-item>
                <el-form-item label="metadata">
                  <el-input
                    v-model="form.metadata"
                    type="textarea"
                    :rows="4"
                    placeholder='JSON，如 {"vocal_gender":"f","control_sliders":{"style_weight":0.87}}'
                  />
                </el-form-item>
              </el-collapse-item>
            </el-collapse>

            <el-form-item style="margin-top: 16px">
              <el-button
                type="primary"
                size="large"
                :loading="submitting"
                :disabled="!balanceEnough"
                @click="submit"
              >
                提交创作（扣 {{ totalCost }} 积分）
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <!-- 生成结果 -->
      <el-col :xs="24" :lg="10" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>本次生成</span></template>

          <el-empty v-if="!tracking.length" description="提交后会在这里显示两个版本的生成进度" :image-size="80" />

          <div v-for="(item, index) in tracking" :key="item.id" class="track-card">
            <div class="track-head">
              <span class="version">版本 {{ index + 1 }}</span>
              <span v-if="item.voice" class="text-muted voice-by"><el-icon><Microphone /></el-icon>{{ item.voice }} 演唱</span>
              <span class="mono text-muted">#{{ item.id }}</span>
              <el-tag :type="TASK_STATUS[item.status]?.type || 'info'" size="small" style="margin-left: auto">
                {{ TASK_STATUS[item.status]?.label || item.status }}
              </el-tag>
            </div>

            <el-progress
              v-if="running(item)"
              :percentage="progressOf(item)"
              :stroke-width="10"
              striped
              striped-flow
              :duration="10"
              style="margin: 10px 0 6px"
            />
            <div v-if="running(item)" class="text-muted" style="font-size: 12px">
              {{ item.voice ? `「${item.voice}」演唱中` : '生成中' }}，已用时 {{ elapsedText(item) }}，每 3 秒自动查询一次
            </div>

            <template v-if="item.status === 'completed' && item.task">
              <div class="result-body">
                <el-image
                  v-if="item.task.fileInfo?.coverUrl"
                  :src="item.task.fileInfo.coverUrl"
                  fit="cover"
                  :preview-src-list="[item.task.fileInfo.coverUrl]"
                  preview-teleported
                  class="cover"
                />
                <div class="result-main">
                  <audio
                    v-if="playableUrl(item.task)"
                    :src="playableUrl(item.task)"
                    controls
                    preload="none"
                    style="width: 100%"
                  ></audio>
                  <p v-if="item.task.fileInfo?.duration" class="text-muted" style="font-size: 12px; margin: 4px 0 0">
                    时长 {{ Math.floor(item.task.fileInfo.duration / 60) }}:{{
                      String(Math.round(item.task.fileInfo.duration % 60)).padStart(2, '0')
                    }}
                  </p>
                </div>
              </div>

              <div class="mono id-row">
                <span class="text-muted">custom_id</span>
                <span class="id">{{ item.task.custom_id }}</span>
                <el-button link type="primary" size="small" @click="copyText(item.task.custom_id)">复制</el-button>
              </div>

              <div style="margin-top: 8px">
                <!-- 延长、翻唱是 Suno 的能力，Mureka 演唱的歌不能用 -->
                <template v-if="!item.voice">
                  <el-button size="small" @click="useAsSource(item.task, 'extend')">用它延长</el-button>
                  <el-button size="small" @click="useAsSource(item.task, 'cover')">用它翻唱</el-button>
                </template>
                <el-button size="small" type="primary" plain @click="$router.push('/songs')">去生成 MV</el-button>
              </div>
            </template>

            <div v-if="item.status === 'failed'" style="color: #f56c6c; font-size: 13px; margin-top: 6px">
              {{ item.task?.error_message || '生成失败' }}，积分不退还
              <el-button link type="primary" size="small" :loading="item.retrying" @click="retryItem(item)">
                免费重试
              </el-button>
            </div>
          </div>
        </el-card>

        <el-card shadow="never" body-style="padding:16px" style="margin-top: 12px">
          <template #header><span>最近创作</span></template>
          <el-table v-loading="historyLoading" :data="history" size="small" style="width: 100%">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="merchant_name" label="商户" min-width="90" show-overflow-tooltip />
            <el-table-column label="状态" width="86" align="center">
              <template #default="{ row }">
                <el-tag :type="TASK_STATUS[row.status]?.type || 'info'" size="small">
                  {{ TASK_STATUS[row.status]?.label || row.status }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="时间" width="150">
              <template #default="{ row }">
                <span class="mono">{{ formatTime(row.created_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="80">
              <template #default="{ row }">
                <el-button v-if="row.custom_id" link type="primary" size="small" @click="copyText(row.custom_id)">
                  复制 ID
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="vocalDialog.visible" title="新建演唱音色" width="560px" destroy-on-close>
      <el-alert type="info" :closable="false" show-icon style="margin-bottom: 14px">
        上传或录制一段 <b>15~30 秒的清唱</b>（只有人声、没有伴奏，安静环境），创建后创作的歌会直接用这个声音演唱。
        超过 30 秒的部分会被裁掉。本次扣 {{ vocalPrices.clone }} 积分。
      </el-alert>
      <el-form label-width="84px" @submit.prevent>
        <el-form-item label="音色名称" required>
          <el-input v-model="vocalDialog.name" maxlength="30" show-word-limit placeholder="如：小王的声音" />
        </el-form-item>
        <el-form-item label="清唱音频" required>
          <AudioInput
            v-model="vocalDialog.audio_url"
            :merchant-id="form.merchant_id"
            hint="15~30 秒清唱，支持 mp3 / m4a / wav，现场录音也可以"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="vocalDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="vocalDialog.creating" @click="submitVocal">
          创建（{{ vocalPrices.clone }} 积分）
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.upload-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  margin: 6px 0 2px;
}
.result-body {
  display: flex;
  gap: 10px;
  align-items: center;
  margin: 8px 0;
}

.cover {
  width: 64px;
  height: 64px;
  border-radius: 6px;
  flex-shrink: 0;
}

.result-main {
  flex: 1;
  min-width: 0;
}

.track-card {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 12px;
  margin-bottom: 12px;

  .track-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .version {
    font-weight: 600;
    font-size: 14px;
  }
}

.voice-by {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 12px;
}

.vocal-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.id-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;

  .id {
    flex: 1;
    word-break: break-all;
  }
}
</style>
