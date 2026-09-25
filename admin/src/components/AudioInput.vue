<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { uploadSample } from '@/api'
import { toWav } from '@/utils/wav'

/**
 * 音频输入：上传文件 / 现场录音 / 粘贴链接，三种方式最终都产出一个可供上游下载的地址。
 * 文件与录音会先传到平台的 COS 存储桶。
 */
const props = defineProps({
  modelValue: { type: String, default: '' },
  merchantId: { type: [Number, String], default: '' },
  modes: { type: Array, default: () => ['upload', 'record', 'url'] },
  hint: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue'])

const MODE_LABELS = { upload: '上传文件', record: '现场录音', url: '音频链接' }
const ACCEPT = '.wav,.mp3,.m4a,.aac,.flac,.ogg'
const MAX_SIZE = 50 * 1024 * 1024

const mode = ref(props.modes[0])
const uploading = ref(false)
const progress = ref(0)
const uploaded = ref(null) // { name, size, preview }
const linkInput = ref('')

const url = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

watch(mode, () => {
  resetRecording()
  if (mode.value === 'url') url.value = linkInput.value.trim()
  else if (!uploaded.value) url.value = ''
})
watch(linkInput, (v) => {
  if (mode.value === 'url') url.value = v.trim()
})

function formatSize(bytes) {
  return bytes > 1024 * 1024 ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : `${Math.ceil(bytes / 1024)} KB`
}

async function upload(blob, name) {
  if (!props.merchantId) return ElMessage.warning('请先选择归属商户')
  if (blob.size > MAX_SIZE) return ElMessage.warning('音频不能超过 50MB')

  const form = new FormData()
  form.append('merchant_id', props.merchantId)
  form.append('file', blob, name)

  uploading.value = true
  progress.value = 0
  try {
    const data = await uploadSample(form, (e) => {
      if (e.total) progress.value = Math.round((e.loaded / e.total) * 100)
    })
    clearPreview()
    uploaded.value = { name, size: blob.size, preview: URL.createObjectURL(blob) }
    url.value = data.url
    ElMessage.success('音频已上传')
  } finally {
    uploading.value = false
  }
}

function clearPreview() {
  if (uploaded.value?.preview) URL.revokeObjectURL(uploaded.value.preview)
}

function reset() {
  clearPreview()
  uploaded.value = null
  url.value = ''
}

/* ---------------------------------- 上传文件 ---------------------------------- */

function onFileChange(file) {
  const ext = file.name.slice(file.name.lastIndexOf('.')).toLowerCase()
  if (!ACCEPT.split(',').includes(ext)) {
    ElMessage.warning('仅支持 wav、mp3、m4a、aac、flac、ogg 格式')
    return
  }
  upload(file.raw, file.name)
}

/* ---------------------------------- 现场录音 ---------------------------------- */

const recording = ref(false)
const seconds = ref(0)
const recorded = ref(null) // { blob, preview }
let recorder = null
let stream = null
let chunks = []
let tick = null

const canRecord = typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && window.isSecureContext

async function startRecording() {
  try {
    // 唱歌录音关闭降噪与自动增益，保留原始音色
    stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false }
    })
  } catch {
    ElMessage.error('无法使用麦克风，请在浏览器中允许麦克风权限')
    return
  }

  resetRecording()
  chunks = []
  recorder = new MediaRecorder(stream)
  recorder.ondataavailable = (e) => e.data.size && chunks.push(e.data)
  recorder.onstop = finishRecording
  recorder.start()
  recording.value = true
  seconds.value = 0
  tick = setInterval(() => seconds.value++, 1000)
}

function stopRecording() {
  if (recorder && recorder.state !== 'inactive') recorder.stop()
}

async function finishRecording() {
  recording.value = false
  clearInterval(tick)
  stream?.getTracks().forEach((t) => t.stop())

  try {
    const wav = await toWav(new Blob(chunks, { type: recorder.mimeType }))
    recorded.value = { blob: wav, preview: URL.createObjectURL(wav) }
  } catch {
    ElMessage.error('录音解析失败，请换用 Chrome 或 Edge 重试')
  }
}

function resetRecording() {
  if (recording.value) stopRecording()
  if (recorded.value?.preview) URL.revokeObjectURL(recorded.value.preview)
  recorded.value = null
}

function useRecording() {
  const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
  upload(recorded.value.blob, `record_${stamp}.wav`)
}

const clock = computed(() => `${String(Math.floor(seconds.value / 60)).padStart(2, '0')}:${String(seconds.value % 60).padStart(2, '0')}`)

onBeforeUnmount(() => {
  clearInterval(tick)
  stream?.getTracks().forEach((t) => t.stop())
  clearPreview()
  if (recorded.value?.preview) URL.revokeObjectURL(recorded.value.preview)
})
</script>

<template>
  <div class="audio-input">
    <!-- 已上传：展示结果，可重新选择 -->
    <div v-if="uploaded && mode !== 'url'" class="done">
      <div class="done-head">
        <el-icon color="#67c23a"><CircleCheckFilled /></el-icon>
        <span class="name" :title="uploaded.name">{{ uploaded.name }}</span>
        <span class="text-muted">{{ formatSize(uploaded.size) }}</span>
        <el-button link type="primary" size="small" @click="reset">重新选择</el-button>
      </div>
      <audio :src="uploaded.preview" controls preload="metadata" class="player" />
    </div>

    <template v-else>
      <el-radio-group v-if="modes.length > 1" v-model="mode" size="small" class="modes">
        <el-radio-button v-for="m in modes" :key="m" :value="m">{{ MODE_LABELS[m] }}</el-radio-button>
      </el-radio-group>

      <!-- 上传文件 -->
      <el-upload
        v-if="mode === 'upload'"
        drag
        :accept="ACCEPT"
        :show-file-list="false"
        :auto-upload="false"
        :disabled="uploading"
        :on-change="onFileChange"
        class="uploader"
      >
        <template v-if="uploading">
          <el-progress type="circle" :percentage="progress" :width="56" />
          <div class="text-muted" style="margin-top: 6px">上传中…</div>
        </template>
        <template v-else>
          <el-icon :size="32" color="#909399"><UploadFilled /></el-icon>
          <div>将音频拖到这里，或<em>点击选择</em></div>
          <div class="text-muted" style="font-size: 12px">支持 wav、mp3、m4a、aac、flac、ogg，不超过 50MB</div>
        </template>
      </el-upload>

      <!-- 现场录音 -->
      <div v-else-if="mode === 'record'" class="recorder">
        <el-alert
          v-if="!canRecord"
          type="warning"
          :closable="false"
          show-icon
          title="当前环境无法录音"
          description="浏览器只允许在 HTTPS 或 localhost 下使用麦克风，请改用上传文件"
        />
        <template v-else>
          <div class="rec-row">
            <el-button v-if="!recording" type="danger" :icon="'Microphone'" :disabled="uploading" @click="startRecording">
              {{ recorded ? '重新录制' : '开始录音' }}
            </el-button>
            <el-button v-else type="danger" plain @click="stopRecording">停止录音</el-button>
            <span v-if="recording" class="rec-clock"><i class="dot" />{{ clock }}</span>
            <span v-else-if="recorded" class="text-muted">已录 {{ clock }}，试听满意后点使用</span>
          </div>
          <template v-if="recorded && !recording">
            <audio :src="recorded.preview" controls preload="metadata" class="player" />
            <el-button type="primary" size="small" :loading="uploading" @click="useRecording">
              {{ uploading ? `上传中 ${progress}%` : '使用这段录音' }}
            </el-button>
          </template>
        </template>
      </div>

      <!-- 音频链接 -->
      <el-input v-else v-model="linkInput" placeholder="https://example.com/audio.mp3" />
    </template>

    <div v-if="hint" class="text-muted hint">{{ hint }}</div>
  </div>
</template>

<style scoped lang="scss">
.audio-input {
  width: 100%;
}

.modes {
  margin-bottom: 8px;
}

.uploader {
  width: 100%;

  :deep(.el-upload-dragger) {
    padding: 16px;
  }
}

.recorder {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}

.rec-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.rec-clock {
  font-family: monospace;
  color: #f56c6c;
  display: inline-flex;
  align-items: center;
  gap: 6px;

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #f56c6c;
    animation: blink 1s infinite;
  }
}

@keyframes blink {
  50% {
    opacity: 0.2;
  }
}

.done {
  border: 1px solid #e1f3d8;
  background: #f0f9eb;
  border-radius: 6px;
  padding: 8px 12px;

  .done-head {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.player {
  width: 100%;
  height: 36px;
  margin-top: 6px;
}

.hint {
  font-size: 12px;
  margin-top: 4px;
}
</style>
