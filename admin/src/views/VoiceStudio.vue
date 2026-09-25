<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  coverWithVoice,
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
const prices = reactive({ train: 0, cover: 0 })
const defaultVoice = ref({ model_name: 'default', name: '官方默认音色' })
let voiceTimer = null

const readyVoices = computed(() => voices.value.filter((v) => v.status === 'completed'))

async function loadVoices() {
  if (!merchantId.value) return
  voicesLoading.value = true
  try {
    const data = await fetchVoices({ merchant_id: merchantId.value })
    voices.value = data.list || []
    configured.value = data.configured
    Object.assign(prices, data.prices || {})
    if (data.default) defaultVoice.value = data.default
  } finally {
    voicesLoading.value = false
  }

  // 有音色在训练时定期刷新，训练完就停
  const training = voices.value.some((v) => v.status === 'pending' || v.status === 'processing')
  if (training && !voiceTimer) voiceTimer = setInterval(loadVoices, 10000)
  if (!training && voiceTimer) {
    clearInterval(voiceTimer)
    voiceTimer = null
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
    ElMessage.success(`已开始训练，扣除 ${data.cost} 积分，余额 ${thousands(data.balance)}`)
    trainVisible.value = false
    loadMerchants()
    loadVoices()
  } finally {
    training.value = false
  }
}

async function removeVoice(voice) {
  await ElMessageBox.confirm(
    `将从音色库移除「${voice.name}」，之后无法再用它翻唱；已扣积分不退还。`,
    '删除音色',
    { type: 'warning', confirmButtonText: '确认删除', confirmButtonClass: 'el-button--danger' }
  )
  await deleteVoice(voice.task_id)
  ElMessage.success('已删除')
  if (form.voice_id === voice.task_id) form.voice_id = 0
  loadVoices()
}

/* ---------------------------------- 翻唱 ---------------------------------- */

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

const balanceEnough = computed(() => !currentMerchant.value || currentMerchant.value.points >= prices.cover)

function validate() {
  if (!merchantId.value) return '请选择归属商户'
  if (form.source === 'song' && !form.song_task_id) return '请选择要翻唱的作品'
  if (form.source === 'url' && !/^https?:\/\//.test(form.audio_url.trim())) return '请上传原曲或填写音频链接'
  return ''
}

async function submitCover() {
  const error = validate()
  if (error) return ElMessage.warning(error)

  submitting.value = true
  try {
    const payload = {
      merchant_id: merchantId.value,
      voice_id: form.voice_id,
      separate: form.separate,
      title: form.title
    }
    if (form.source === 'song') payload.song_task_id = form.song_task_id
    else payload.audio_url = form.audio_url.trim()

    const data = await coverWithVoice(payload)
    ElMessage.success(`已提交，扣除 ${data.cost} 积分，余额 ${thousands(data.balance)}`)
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

// 生成失败不退积分，可用原参数免费重试，任务编号不变
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
    const data = await fetchTasks({ kind: 'voice_cover', merchant_id: merchantId.value || undefined, page: 1, size: 10 })
    history.value = data.list || []
  } finally {
    historyLoading.value = false
  }
}

watch(merchantId, () => {
  form.voice_id = 0
  form.song_task_id = null
  loadVoices()
  searchSongs()
  loadHistory()
})

onMounted(loadMerchants)

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  if (voiceTimer) clearInterval(voiceTimer)
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>音色翻唱</h2>
        <p class="desc">保留原曲旋律与伴奏，把人声换成指定音色；可上传一段唱歌干声训练专属音色</p>
      </div>
      <el-button :icon="'Refresh'" @click="loadVoices(), loadHistory()">刷新</el-button>
    </div>

    <el-alert
      v-if="!configured"
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

          <el-form label-width="96px">
            <el-form-item label="归属商户" required>
              <el-select v-model="merchantId" filterable placeholder="请选择商户" style="width: 100%">
                <el-option v-for="m in merchants" :key="m.id" :label="m.name" :value="m.id" />
              </el-select>
              <div v-if="currentMerchant" class="text-muted hint">
                当前余额 {{ thousands(currentMerchant.points) }} 积分，本次消耗 {{ prices.cover }} 积分
                <span v-if="!balanceEnough" style="color: #f56c6c">（余额不足，请先充值）</span>
              </div>
            </el-form-item>

            <el-form-item label="音色" required>
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
                hint="上传要翻唱的歌曲；填链接时需能被腾讯服务直接下载"
              />
            </el-form-item>

            <el-form-item label="人声分离">
              <el-switch v-model="form.separate" />
              <span class="text-muted" style="margin-left: 8px; font-size: 12px">
                原曲带伴奏时请开启，先分离出人声再换音色
              </span>
            </el-form-item>

            <el-form-item label="备注名">
              <el-input v-model="form.title" maxlength="100" placeholder="选填，便于在任务列表里辨认" />
            </el-form-item>

            <el-form-item style="margin-top: 16px">
              <el-button type="primary" :loading="submitting" :disabled="!balanceEnough" @click="submitCover">
                开始翻唱（{{ prices.cover }} 积分）
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
              {{ item.task?.error_message || '翻唱失败' }}，积分不退还
              <el-button link type="primary" size="small" :loading="item.retrying" @click="retryItem(item)">
                免费重试
              </el-button>
            </div>
          </div>
        </el-card>

        <el-card shadow="never" body-style="padding:16px" style="margin-top: 12px">
          <template #header><span>最近翻唱</span></template>
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
    <el-card shadow="never" body-style="padding:16px">
      <template #header>
        <div class="card-head">
          <span>音色库<span class="text-muted" style="font-size: 12px; margin-left: 8px">当前商户专属音色</span></span>
          <el-button type="primary" size="small" :icon="'Plus'" :disabled="!merchantId" @click="openTrain">
            训练新音色（{{ prices.train }} 积分）
          </el-button>
        </div>
      </template>

      <el-table v-loading="voicesLoading" :data="voices" size="small" style="width: 100%">
        <el-table-column prop="name" label="音色名称" min-width="120" />
        <el-table-column label="模型名" min-width="150">
          <template #default="{ row }"><span class="mono">{{ row.model_name }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tooltip v-if="row.status === 'failed'" :content="row.error_message || '训练失败'" placement="top">
              <el-tag type="danger" size="small">训练失败</el-tag>
            </el-tooltip>
            <el-tag v-else :type="TASK_STATUS[row.status]?.type || 'info'" size="small">
              {{ row.status === 'completed' ? '可用' : '训练中' }}
            </el-tag>
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
          开始训练（{{ prices.train }} 积分）
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
</style>
