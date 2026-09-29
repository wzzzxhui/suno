<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  coverWithVoice,
  coverWithVocal,
  createVocal,
  fetchVocals,
  deleteVoice,
  fetchMerchants,
  fetchSongs,
  fetchTaskDetail,
  fetchTasks,
  fetchVoices,
  retryTask,
  trainVoice
} from '@/api'
import { TASK_STATUS, formatTime, playableUrl, thousands } from '@/utils/format'
import AudioInput from '@/components/AudioInput.vue'

const merchants = ref([])
const merchantId = ref('')
const currentMerchant = computed(() => merchants.value.find((m) => m.id === merchantId.value))

async function loadMerchants() {
  const data = await fetchMerchants({ page: 1, size: 200, status: 1 })
  merchants.value = data.list || []
  if (!merchantId.value && merchants.value.length) merchantId.value = merchants.value[0].id
}

/* ---------------------------------- 音色库 ---------------------------------- */

const voices = ref([])
const voicesLoading = ref(false)
const configured = ref(true)
const defaultVoice = ref({ model_name: 'default', name: '官方默认音色' })
const trainTimeout = ref(180) // 分钟，超过仍未完成会被判失败
// 每轮训练耗时：有完成记录时为实际平均值（samples > 0），否则为配置的默认值
const trainPace = ref({ seconds_per_epoch: 20, samples: 0 })
let voiceTimer = null

// 训练中的音色每秒刷新一次「已训练多久 / 多久前确认」
const now = ref(Date.now())
let clockTimer = null
const trainingVoices = computed(() => voices.value.filter((v) => v.status === 'pending' || v.status === 'processing'))

function minutesText(ms) {
  const min = Math.floor(ms / 60000)
  if (min < 1) return '不到 1 分钟'
  if (min < 60) return `${min} 分钟`
  return min % 60 ? `${Math.floor(min / 60)} 小时 ${min % 60} 分钟` : `${min / 60} 小时`
}

// 进度是估算值，悬停说明依据
const paceHint = computed(() => {
  const sec = Math.round(trainPace.value.seconds_per_epoch)
  return trainPace.value.samples > 0
    ? `预计进度：按最近 ${trainPace.value.samples} 次训练的平均耗时（每轮约 ${sec} 秒）估算，以腾讯云返回完成为准`
    : `预计进度：暂无训练完成记录，按每轮约 ${sec} 秒估算，完成一次训练后会改用实际耗时`
})

function agoText(time) {
  const sec = Math.max(0, Math.round((now.value - new Date(time).getTime()) / 1000))
  if (sec < 60) return `${sec} 秒前`
  return `${Math.floor(sec / 60)} 分钟前`
}

/**
 * 训练进度：阶段、已训练时长、轮次与估算百分比。
 * 腾讯不返回进度，按「每轮耗时 × 轮次」估算；完成前最多显示 95%，以腾讯返回完成为准。
 */
function trainInfo(v) {
  const p = v.progress
  const stage = p?.stage === 'running' ? 'running' : 'queued'
  const since = p?.started_at ? now.value - new Date(p.started_at).getTime() : 0
  const epochs = p?.total_epoch || 30
  const expected = trainPace.value.seconds_per_epoch * epochs * 1000
  const ratio = stage === 'running' && expected > 0 ? since / expected : 0
  const percent = Math.min(95, Math.max(stage === 'running' ? 1 : 0, Math.floor(ratio * 100)))
  let eta = '等待腾讯云开始训练'
  if (stage === 'running') eta = ratio < 1 ? `预计还需 ${minutesText(expected - since)}` : '比预计慢，仍在训练中'
  return {
    stage,
    percent,
    eta,
    overdue: ratio >= 1,
    label: stage === 'running' ? '训练中' : '排队中',
    detail:
      stage === 'running'
        ? `已训练 ${minutesText(since)}${p?.total_epoch ? ` · 共 ${p.total_epoch} 轮` : ''}`
        : `已等待 ${minutesText(now.value - new Date(v.created_at).getTime())}`,
    checked: agoText(v.checked_at),
    // 后端每几秒查一次上游；两分钟没更新说明后端可能没在运行
    stale: now.value - new Date(v.checked_at).getTime() > 2 * 60 * 1000
  }
}

const readyVoices = computed(() => voices.value.filter((v) => v.status === 'completed'))

async function loadVoices() {
  if (!merchantId.value) return
  voicesLoading.value = true
  try {
    const data = await fetchVoices({ merchant_id: merchantId.value })
    voices.value = data.list || []
    configured.value = data.configured
    if (data.default) defaultVoice.value = data.default
    if (data.train_timeout_minutes) trainTimeout.value = data.train_timeout_minutes
    if (data.train_pace?.seconds_per_epoch) trainPace.value = data.train_pace
    now.value = Date.now()
  } finally {
    voicesLoading.value = false
  }

  // 有音色在训练时定期刷新，训练完就停
  const training = trainingVoices.value.length > 0
  if (training && !voiceTimer) {
    voiceTimer = setInterval(loadVoices, 10000)
    clockTimer = setInterval(() => (now.value = Date.now()), 1000)
  }
  if (!training && voiceTimer) {
    clearInterval(voiceTimer)
    clearInterval(clockTimer)
    voiceTimer = clockTimer = null
  }
}

const trainVisible = ref(false)
const training = ref(false)
const trainForm = reactive({ name: '', audio_url: '', total_epoch: 30 })

function openTrain() {
  Object.assign(trainForm, { name: '', audio_url: '', total_epoch: 30 })
  trainVisible.value = true
}

async function submitTrain() {
  if (!trainForm.name.trim()) return ElMessage.warning('请填写音色名称')
  if (!/^https?:\/\//.test(trainForm.audio_url.trim())) return ElMessage.warning('请上传、录制或填写干声音频')

  training.value = true
  try {
    const data = await trainVoice({ merchant_id: merchantId.value, ...trainForm })
    ElMessage.success('已开始训练')
    trainVisible.value = false
    loadMerchants()
    loadVoices()
  } finally {
    training.value = false
  }
}

async function removeVoice(voice) {
  await ElMessageBox.confirm(
    `将从音色库移除「${voice.name}」，之后无法再用它翻唱。`,
    '删除音色',
    { type: 'warning', confirmButtonText: '确认删除', confirmButtonClass: 'el-button--danger' }
  )
  await deleteVoice(voice.task_id)
  ElMessage.success('已删除')
  if (form.voice_id === voice.task_id) form.voice_id = 0
  loadVoices()
}

/* ---------------------------------- 翻唱 ---------------------------------- */

const coverMode = ref('suno')
const advancedVocals = ref([])
const advancedVocalDialog = reactive({ visible: false, name: '', audio_url: '', creating: false })
const advancedForm = reactive({ voice_id: null, lyrics: '', title: '' })

async function loadAdvancedVocals() {
  if (!merchantId.value) return
  const data = await fetchVocals({ merchant_id: merchantId.value })
  advancedVocals.value = (data.list || []).filter((v) => v.status === 'completed')
  if (advancedForm.voice_id && !advancedVocals.value.some((v) => v.task_id === advancedForm.voice_id)) {
    advancedForm.voice_id = null
  }
}

async function createAdvancedVocal() {
  if (!advancedVocalDialog.name.trim()) return ElMessage.warning('请填写演唱音色名称')
  if (!/^https?:\/\//.test(advancedVocalDialog.audio_url.trim())) return ElMessage.warning('请上传或录制清唱样本')
  advancedVocalDialog.creating = true
  try {
    const data = await createVocal({
      merchant_id: merchantId.value,
      name: advancedVocalDialog.name.trim(),
      audio_url: advancedVocalDialog.audio_url.trim()
    })
    await loadAdvancedVocals()
    advancedForm.voice_id = data.task_id
    advancedVocalDialog.visible = false
    ElMessage.success('演唱音色已创建')
  } finally {
    advancedVocalDialog.creating = false
  }
}

const form = reactive({
  voice_id: 0,
  source: 'song', // song：作品库；url：音频链接
  song_task_id: null,
  audio_url: '',
  separate: true,
  title: ''
})
const submitting = ref(false)

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


function validate() {
  if (!merchantId.value) return '请选择归属商户'
  if (form.source === 'song' && !form.song_task_id) return '请选择要翻唱的作品'
  if (form.source === 'url' && !/^https?:\/\//.test(form.audio_url.trim())) return '请上传原曲或填写音频链接'
  if (coverMode.value === 'mureka') {
    if (!advancedForm.voice_id) return '请选择或创建高级演唱音色'
    if (!advancedForm.title.trim()) return '请填写歌名'
    if (!advancedForm.lyrics.trim()) return '请填写原曲歌词'
  }
  return ''
}

async function submitCover() {
  const error = validate()
  if (error) return ElMessage.warning(error)

  submitting.value = true
  try {
    const advanced = coverMode.value === 'mureka'
    const payload = advanced
      ? { merchant_id: merchantId.value, voice_id: advancedForm.voice_id, title: advancedForm.title.trim(), lyrics: advancedForm.lyrics.trim() }
      : { merchant_id: merchantId.value, voice_id: form.voice_id, separate: form.separate, title: form.title }
    if (form.source === 'song') payload.song_task_id = form.song_task_id
    else payload.audio_url = form.audio_url.trim()

    const data = advanced ? await coverWithVocal(payload) : await coverWithVoice(payload)
    ElMessage.success('已提交')
    startTracking(data.task_ids)
    loadMerchants()
    loadHistory()
  } finally {
    submitting.value = false
  }
}

/* ---------------------------------- 轮询 ---------------------------------- */

const tracking = ref([])
let timer = null

// 生成失败不退积分，可用原参数重试，任务编号不变
async function retryItem(item) {
  item.retrying = true
  try {
    await retryTask(item.id)
    Object.assign(item, { status: 'pending', elapsed: 0, task: null })
    ElMessage.success('已重新提交')
    if (!timer) timer = setInterval(tick, 3000)
    tick()
  } finally {
    item.retrying = false
  }
}

function startTracking(ids) {
  tracking.value = ids.map((id) => ({ id, status: 'pending', elapsed: 0, task: null }))
  if (!timer) timer = setInterval(tick, 3000)
  tick()
}

async function tick() {
  const pending = tracking.value.filter((t) => !['completed', 'failed'].includes(t.status))
  if (!pending.length) {
    clearInterval(timer)
    timer = null
    loadHistory()
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
        // 单次查询失败不中断轮询
      }
    })
  )
}

/* ---------------------------------- 历史 ---------------------------------- */

const history = ref([])
const historyLoading = ref(false)

async function loadHistory() {
  historyLoading.value = true
  try {
    const data = await fetchTasks({ kind: coverMode.value === 'mureka' ? 'voice_song' : 'voice_cover', merchant_id: merchantId.value || undefined, page: 1, size: 10 })
    history.value = data.list || []
  } finally {
    historyLoading.value = false
  }
}

watch(merchantId, () => {
  form.voice_id = 0
  form.song_task_id = null
  advancedForm.voice_id = null
  loadAdvancedVocals()
  loadVoices()
  searchSongs()
  loadHistory()
})

watch(coverMode, loadHistory)

onMounted(loadMerchants)

onBeforeUnmount(() => {
  if (clockTimer) clearInterval(clockTimer)
  if (timer) clearInterval(timer)
  if (voiceTimer) clearInterval(voiceTimer)
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>音色翻唱</h2>
        <p class="desc">选择普通模式或高级模式，使用指定音色处理原曲。</p>
      </div>
      <el-button :icon="'Refresh'" @click="loadVoices(), loadHistory()">刷新</el-button>
    </div>

    <el-alert
      v-if="coverMode === 'suno' && !configured"
      type="warning"
      show-icon
      :closable="false"
      style="margin-bottom: 12px"
      title="尚未配置腾讯唱歌克隆密钥，当前为本地模拟结果，仅用于联调界面"
      description="在后端 .env 中填写 TME_* 配置并重启服务后即对接真实接口"
    />

    <el-row :gutter="12">
      <!-- 翻唱表单 -->
      <el-col :xs="24" :lg="14" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>翻唱参数</span></template>

          <el-tabs v-model="coverMode" style="margin-bottom: 12px">
            <el-tab-pane label="普通模式" name="suno" />
            <el-tab-pane label="高级模式" name="mureka" />
          </el-tabs>
          <el-alert v-if="coverMode === 'mureka'" type="info" :closable="false" style="margin-bottom: 16px"
            title="参考原曲重新演唱"
            description="高级模式使用原曲前约 30 秒作音乐参考，按填写的歌词和自定义音色重新生成。编曲、旋律和时长可能变化，不保证保留原伴奏。" />
          <p v-if="coverMode === 'mureka'" style="margin: -8px 0 16px">
            <router-link :to="{ path: '/guide', query: { tool: 'cover_advanced' } }" target="_blank">查看高级模式参考重唱教程 ↗</router-link>
          </p>

          <el-form label-width="96px">
            <el-form-item label="归属商户" required>
              <el-select v-model="merchantId" filterable placeholder="请选择商户" style="width: 100%">
                <el-option v-for="m in merchants" :key="m.id" :label="m.name" :value="m.id" />
              </el-select>
              <div v-if="currentMerchant" class="text-muted hint">
                由平台账号处理
              </div>
            </el-form-item>

            <el-form-item v-if="coverMode === 'suno'" label="音色" required>
              <el-select v-model="form.voice_id" style="width: 100%">
                <el-option :value="0" :label="defaultVoice.name">
                  <span>{{ defaultVoice.name }}</span>
                  <span class="text-muted" style="float: right">无需训练</span>
                </el-option>
                <el-option v-for="v in readyVoices" :key="v.task_id" :value="v.task_id" :label="v.name">
                  <span>{{ v.name }}</span>
                  <span class="mono text-muted" style="float: right">{{ v.model_name }}</span>
                </el-option>
              </el-select>
              <div class="text-muted hint">想用自己的声音？在下方音色库上传唱歌干声训练专属音色</div>
            </el-form-item>

            <el-form-item v-if="coverMode === 'mureka'" label="演唱音色" required>
              <div style="width: 100%">
                <el-select v-model="advancedForm.voice_id" placeholder="选择已创建的演唱音色" style="width: 100%">
                  <el-option v-for="v in advancedVocals" :key="v.task_id" :value="v.task_id" :label="v.name" />
                </el-select>
                <el-button link type="primary" @click="advancedVocalDialog.visible = true">上传清唱并创建音色</el-button>
              </div>
            </el-form-item>

            <el-form-item label="原曲来源">
              <el-radio-group v-model="form.source">
                <el-radio-button value="song">作品库</el-radio-button>
                <el-radio-button value="url">上传音频</el-radio-button>
              </el-radio-group>
            </el-form-item>

            <el-form-item v-if="form.source === 'song'" label="选择作品" required>
              <el-select
                v-model="form.song_task_id"
                filterable
                remote
                :remote-method="searchSongs"
                :loading="songsLoading"
                placeholder="输入歌名或 custom_id 搜索该商户的作品"
                style="width: 100%"
              >
                <el-option v-for="s in songs" :key="s.task_id" :value="s.task_id" :label="s.title || s.custom_id">
                  <span>{{ s.title || '未命名' }}</span>
                  <span class="mono text-muted" style="float: right">#{{ s.task_id }}</span>
                </el-option>
              </el-select>
            </el-form-item>

            <el-form-item v-else label="原曲音频" required>
              <AudioInput
                :key="`cover-${merchantId}`"
                v-model="form.audio_url"
                :merchant-id="merchantId"
                :modes="['upload', 'url']"
                :hint="coverMode === 'mureka' ? '上传歌曲或填写可访问的音频链接；高级模式会截取约 30 秒作为参考' : '上传要翻唱的歌曲；填链接时需能被服务直接下载'"
              />
            </el-form-item>

            <el-form-item v-if="coverMode === 'suno'" label="人声分离">
              <el-switch v-model="form.separate" />
              <span class="text-muted" style="margin-left: 8px; font-size: 12px">
                原曲带伴奏时请开启，先分离出人声再换音色
              </span>
            </el-form-item>

            <el-form-item v-if="coverMode === 'suno'" label="备注名">
              <el-input v-model="form.title" maxlength="100" placeholder="选填，便于在任务列表里辨认" />
            </el-form-item>

            <template v-if="coverMode === 'mureka'">
              <el-form-item label="歌名" required>
                <el-input v-model="advancedForm.title" maxlength="100" placeholder="填写新作品名称" />
              </el-form-item>
              <el-form-item label="歌词" required>
                <el-input v-model="advancedForm.lyrics" type="textarea" :rows="10" placeholder="按原分行粘贴歌词，可保留 [Verse]、[Chorus] 等段落标记" />
              </el-form-item>
            </template>

            <el-form-item style="margin-top: 16px">
              <el-button type="primary" :loading="submitting" @click="submitCover">
                {{ coverMode === 'mureka' ? '开始参考重唱' : '开始翻唱' }}
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <!-- 结果 -->
      <el-col :xs="24" :lg="10" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>本次翻唱</span></template>

          <el-empty v-if="!tracking.length" description="提交后会在这里显示翻唱进度" :image-size="80" />

          <div v-for="item in tracking" :key="item.id" class="track-card">
            <div class="track-head">
              <span class="mono text-muted">#{{ item.id }}</span>
              <el-tag :type="TASK_STATUS[item.status]?.type || 'info'" size="small" style="margin-left: auto">
                {{ TASK_STATUS[item.status]?.label || item.status }}
              </el-tag>
            </div>
            <el-progress
              v-if="item.status === 'pending' || item.status === 'processing'"
              :percentage="Math.min(95, item.elapsed * 2)"
              :show-text="false"
              :stroke-width="4"
              style="margin: 8px 0"
            />
            <div v-if="item.status === 'pending' || item.status === 'processing'" class="text-muted hint">
              已等待 {{ item.elapsed }} 秒，每 3 秒自动查询一次
            </div>
            <audio
              v-if="item.status === 'completed' && playableUrl(item.task)"
              :src="playableUrl(item.task)"
              controls
              preload="none"
              style="width: 100%; margin-top: 8px"
            ></audio>
            <div v-if="item.status === 'failed'" class="error">
              {{ item.task?.error_message || '翻唱失败' }}
              <el-button link type="primary" size="small" :loading="item.retrying" @click="retryItem(item)">
                重试
              </el-button>
            </div>
          </div>
        </el-card>

        <el-card shadow="never" body-style="padding:16px" style="margin-top: 12px">
          <template #header><span>{{ coverMode === 'mureka' ? '最近高级模式作品' : '最近翻唱' }}</span></template>
          <el-table v-loading="historyLoading" :data="history" size="small" style="width: 100%">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column label="状态" width="86" align="center">
              <template #default="{ row }">
                <el-tag :type="TASK_STATUS[row.status]?.type || 'info'" size="small">
                  {{ TASK_STATUS[row.status]?.label || row.status }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="试听" min-width="160">
              <template #default="{ row }">
                <audio v-if="playableUrl(row)" :src="playableUrl(row)" controls preload="none" class="mini-player" />
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="时间" width="150">
              <template #default="{ row }">
                <span class="mono">{{ formatTime(row.created_at) }}</span>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <!-- 音色库 -->
    <el-card v-if="coverMode === 'suno'" shadow="never" body-style="padding:16px">
      <template #header>
        <div class="card-head">
          <span>音色库<span class="text-muted" style="font-size: 12px; margin-left: 8px">当前商户专属音色</span></span>
          <el-button type="primary" size="small" :icon="'Plus'" :disabled="!merchantId" @click="openTrain">
            训练新音色
          </el-button>
        </div>
      </template>

      <el-alert v-if="trainingVoices.length" type="info" :closable="false" show-icon class="train-tip">
        {{ trainingVoices.length }} 个音色正在腾讯云训练，耗时与干声长度和训练轮次有关，50 轮一般十几分钟；
        超过 {{ minutesText(trainTimeout * 60000) }} 仍未完成会判为失败。本页每 10 秒自动刷新，可以离开稍后再来。
      </el-alert>

      <el-table v-loading="voicesLoading && !voices.length" :data="voices" size="small" style="width: 100%">
        <el-table-column prop="name" label="音色名称" min-width="120" />
        <el-table-column label="模型名" min-width="150">
          <template #default="{ row }"><span class="mono">{{ row.model_name }}</span></template>
        </el-table-column>
        <el-table-column label="状态 / 进度" min-width="360">
          <template #default="{ row }">
            <el-tooltip v-if="row.status === 'failed'" :content="row.error_message || '训练失败'" placement="top">
              <el-tag type="danger" size="small">训练失败</el-tag>
            </el-tooltip>
            <el-tag v-else-if="row.status === 'completed'" type="success" size="small">可用</el-tag>
            <div v-else class="train-status">
              <el-tag :type="trainInfo(row).stage === 'running' ? 'warning' : 'info'" size="small">
                <span class="pulse"></span>{{ trainInfo(row).label }}
              </el-tag>
              <span class="train-detail">{{ trainInfo(row).detail }}</span>
              <div class="train-progress">
                <el-progress
                  :percentage="trainInfo(row).percent"
                  :stroke-width="8"
                  :status="trainInfo(row).overdue ? 'warning' : ''"
                  :striped="trainInfo(row).stage === 'running'"
                  striped-flow
                  :duration="12"
                />
                <el-tooltip placement="top" :content="paceHint">
                  <span class="train-eta">{{ trainInfo(row).eta }}</span>
                </el-tooltip>
              </div>
              <div class="train-checked" :class="{ stale: trainInfo(row).stale }">
                <template v-if="trainInfo(row).stale">
                  {{ trainInfo(row).checked }}最后一次确认，后端可能没有在运行
                </template>
                <template v-else>腾讯云状态 {{ trainInfo(row).checked }}确认</template>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }"><span class="mono">{{ formatTime(row.created_at) }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="140">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'completed'"
              link
              type="primary"
              size="small"
              @click="form.voice_id = row.task_id"
            >
              用它翻唱
            </el-button>
            <el-button link type="danger" size="small" @click="removeVoice(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="还没有专属音色，点击右上角训练一个" :image-size="60" />
        </template>
      </el-table>
    </el-card>

    <el-dialog v-model="advancedVocalDialog.visible" title="创建高级演唱音色" width="560px" destroy-on-close>
      <el-form label-width="88px">
        <el-form-item label="名称" required>
          <el-input v-model="advancedVocalDialog.name" maxlength="30" placeholder="如：我的演唱音色" />
        </el-form-item>
        <el-form-item label="清唱样本" required>
          <AudioInput v-model="advancedVocalDialog.audio_url" :merchant-id="merchantId"
            hint="上传或录制 15～30 秒无伴奏清唱；建议单人、少混响" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="advancedVocalDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="advancedVocalDialog.creating" @click="createAdvancedVocal">创建音色</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="trainVisible" title="训练新音色" width="560px" destroy-on-close>
      <el-form label-width="88px">
        <el-form-item label="音色名称" required>
          <el-input v-model="trainForm.name" maxlength="30" show-word-limit placeholder="如：小王的声音" />
        </el-form-item>
        <el-form-item label="干声音频" required>
          <AudioInput
            v-model="trainForm.audio_url"
            :merchant-id="merchantId"
            hint="本人清唱、无伴奏、无混响、单人；录音请在安静环境、离麦克风一拳距离，时长越充足效果越好"
          />
        </el-form-item>
        <el-form-item label="训练轮次">
          <el-input-number v-model="trainForm.total_epoch" :min="1" :max="50" />
          <span class="text-muted" style="margin-left: 8px; font-size: 12px">默认 30，轮次越多越慢</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="trainVisible = false">取消</el-button>
        <el-button type="primary" :loading="training" @click="submitTrain">
          开始训练
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.hint {
  font-size: 12px;
  margin-top: 4px;
  width: 100%;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
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
}

.error {
  color: #f56c6c;
  font-size: 13px;
  margin-top: 6px;
}

.mini-player {
  width: 100%;
  height: 32px;
}

.train-tip {
  margin-bottom: 12px;
}

.train-status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 8px;
  padding: 2px 0;

  .train-detail {
    font-size: 12px;
    color: #606266;
  }

  .train-progress {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;

    .el-progress {
      flex: 1;
      max-width: 260px;
    }

    .train-eta {
      font-size: 12px;
      color: #606266;
      white-space: nowrap;
      cursor: help;
      border-bottom: 1px dashed #c0c4cc;
    }
  }

  .train-checked {
    width: 100%;
    font-size: 11px;
    color: #909399;

    &.stale {
      color: #e6a23c;
    }
  }

  // 训练中的呼吸点，表示正在进行
  .pulse {
    display: inline-block;
    width: 6px;
    height: 6px;
    margin-right: 5px;
    border-radius: 50%;
    background: currentColor;
    vertical-align: middle;
    animation: pulse 1.4s ease-in-out infinite;
  }
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.25;
  }
}
</style>
