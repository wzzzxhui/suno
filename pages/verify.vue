<script setup lang="ts">
import type { ApiResponse } from '~/types/api'

useHead({ title: '创作证明核验 - SUNO API 开放平台' })

interface VerifyResult {
  certificate_no: string
  title: string
  author: string
  suno_id: string
  duration: number
  audio_sha256: string
  song_created_at: string
  issued_at: string
  issuer: string
}

const route = useRoute()
const router = useRouter()

const no = ref(String(route.query.no ?? '').trim())
const loading = ref(false)
const result = ref<VerifyResult | null>(null)
const error = ref('')

const verify = async () => {
  const value = no.value.trim()
  error.value = ''
  result.value = null
  compare.value = null
  if (!value) {
    error.value = '请输入证书编号'
    return
  }

  loading.value = true
  // 编号写进地址栏，核验结果可以直接分享
  router.replace({ query: { no: value } })
  try {
    const res = await $fetch<ApiResponse<VerifyResult>>('/api/verify', { query: { no: value }, ignoreResponseError: true })
    if (res?.success && res.data) {
      result.value = res.data
    } else {
      error.value = res?.code === 404 ? '未查询到该证书，请核对证书编号是否正确' : res?.message || '核验失败，请稍后重试'
    }
  } catch {
    error.value = '核验服务暂时不可用，请稍后重试'
  } finally {
    loading.value = false
  }
}

/* ---------------- 本地比对音频指纹 ---------------- */

// 在浏览器本地计算 SHA-256，文件不会上传
const compare = ref<{ name: string; hash: string; match: boolean } | null>(null)
const hashing = ref(false)

const onFile = async (event: Event) => {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file || !result.value) return
  hashing.value = true
  try {
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
    const hash = Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('')
    compare.value = { name: file.name, hash, match: hash === result.value.audio_sha256 }
  } finally {
    hashing.value = false
    ;(event.target as HTMLInputElement).value = ''
  }
}

const formatTime = (value: string) =>
  new Date(value).toLocaleString('zh-CN', { hour12: false })

const formatDuration = (sec: number) => {
  if (!sec) return '—'
  const total = Math.round(sec)
  return `${Math.floor(total / 60)} 分 ${String(total % 60).padStart(2, '0')} 秒`
}

const groupHash = (hash: string) => hash.match(/.{1,8}/g)?.join(' ') ?? hash

onMounted(() => {
  if (no.value) verify()
})
</script>

<template>
  <div class="py-12">
    <div class="max-w-3xl mx-auto px-4 sm:px-6 lg:px-8">
      <header class="text-center mb-10">
        <h1 class="heading-lg mb-4">创作证明核验</h1>
        <p class="text-gray-400 leading-7">
          输入证书上的编号，或用手机扫描证书二维码，核验音乐作品创作证明的真伪。
        </p>
      </header>

      <form class="card flex flex-col sm:flex-row gap-3" @submit.prevent="verify">
        <input
          v-model="no"
          class="input font-mono uppercase flex-1"
          placeholder="证书编号，如 SC20260925-7K3QM9XA"
          maxlength="32"
          autocomplete="off"
          spellcheck="false"
        />
        <button type="submit" class="btn-primary sm:w-32" :disabled="loading">
          {{ loading ? '核验中…' : '核验' }}
        </button>
      </form>

      <div v-if="error" class="card mt-6 border-red-500/40 text-red-300 flex items-start gap-3">
        <span class="text-xl leading-none">✕</span>
        <p class="leading-6">{{ error }}</p>
      </div>

      <section v-if="result" class="card mt-6">
        <div class="flex items-center gap-3 pb-4 mb-4 border-b border-suno-gray-700">
          <span
            class="w-9 h-9 rounded-full bg-emerald-500/15 text-emerald-400 flex items-center justify-center text-lg"
            >✓</span
          >
          <div>
            <p class="font-bold text-white">证书有效</p>
            <p class="text-xs text-gray-400">由「{{ result.issuer }}」签发，以下为签发时登记的作品信息</p>
          </div>
        </div>

        <dl class="grid grid-cols-[7rem_1fr] gap-y-3 text-sm">
          <dt class="text-gray-400">证书编号</dt>
          <dd class="font-mono text-white">{{ result.certificate_no }}</dd>
          <dt class="text-gray-400">作品名称</dt>
          <dd class="text-white">《{{ result.title }}》</dd>
          <dt class="text-gray-400">署名作者</dt>
          <dd class="text-white">{{ result.author }}</dd>
          <dt class="text-gray-400">作品 ID</dt>
          <dd class="font-mono text-gray-300 break-all">{{ result.suno_id }}</dd>
          <dt class="text-gray-400">作品时长</dt>
          <dd class="text-gray-300">{{ formatDuration(result.duration) }}</dd>
          <dt class="text-gray-400">创作完成时间</dt>
          <dd class="text-gray-300">{{ formatTime(result.song_created_at) }}</dd>
          <dt class="text-gray-400">签发时间</dt>
          <dd class="text-gray-300">{{ formatTime(result.issued_at) }}</dd>
          <dt class="text-gray-400">音频指纹</dt>
          <dd class="font-mono text-gray-300 break-all">
            {{ groupHash(result.audio_sha256) }}
            <span class="block text-xs text-gray-500 mt-1">SHA-256</span>
          </dd>
        </dl>

        <div class="mt-6 pt-5 border-t border-suno-gray-700">
          <p class="text-sm text-white font-semibold mb-1">比对音频文件</p>
          <p class="text-xs text-gray-400 mb-3 leading-5">
            选择从平台下载的 MP3 文件，在浏览器本地计算指纹并与证书比对，文件不会上传。
            重新编码、剪辑或转换过格式的文件指纹会不一致。
          </p>
          <label class="btn-ghost cursor-pointer text-sm">
            {{ hashing ? '计算中…' : '选择音频文件' }}
            <input type="file" accept="audio/*" class="hidden" :disabled="hashing" @change="onFile" />
          </label>

          <div
            v-if="compare"
            class="mt-4 rounded-lg px-4 py-3 text-sm"
            :class="compare.match ? 'bg-emerald-500/10 text-emerald-300' : 'bg-red-500/10 text-red-300'"
          >
            <p class="font-semibold">
              {{ compare.match ? '指纹一致：该文件与证书登记的音频相同' : '指纹不一致：该文件不是证书登记的音频，或已被改动' }}
            </p>
            <p class="font-mono text-xs mt-1 break-all opacity-80">{{ compare.name }}：{{ compare.hash }}</p>
          </div>
        </div>
      </section>

      <p class="text-xs text-gray-500 text-center mt-8 leading-5">
        创作证明依据平台系统记录出具，用于证明作品的创作时间、来源及音频文件指纹，不替代著作权登记证书。
      </p>
    </div>
  </div>
</template>
