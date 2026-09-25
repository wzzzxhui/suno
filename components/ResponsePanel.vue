<script setup lang="ts">
import { toPrettyJson, highlightJson, collectMediaUrls } from '~/utils/format'

const props = defineProps<{
  status?: number
  duration?: number
  requestUrl?: string
  payload?: unknown
  loading?: boolean
  error?: string
}>()

const pretty = computed(() => toPrettyJson(props.payload))
const highlighted = computed(() => highlightJson(pretty.value))
const media = computed(() => (props.payload ? collectMediaUrls(props.payload) : { audio: [], video: [], image: [] }))

const copied = ref(false)
const copy = async () => {
  try {
    await navigator.clipboard.writeText(pretty.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {
    copied.value = false
  }
}

const statusClass = computed(() => {
  const s = props.status ?? 0
  if (s >= 200 && s < 300) return 'bg-emerald-900/40 text-emerald-300 border-emerald-700'
  if (s >= 400 && s < 500) return 'bg-amber-900/40 text-amber-300 border-amber-700'
  if (s >= 500) return 'bg-red-900/40 text-red-300 border-red-700'
  return 'bg-suno-gray-700 text-gray-300 border-suno-gray-600'
})
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between gap-3 mb-3">
      <h3 class="heading-md text-base">响应结果</h3>
      <div class="flex items-center gap-2">
        <span v-if="status" class="tag" :class="statusClass">HTTP {{ status }}</span>
        <span v-if="duration" class="text-xs text-gray-500 font-mono">{{ duration }} ms</span>
        <button v-if="payload" class="text-xs text-gray-400 hover:text-suno-yellow transition-colors" @click="copy">
          {{ copied ? '已复制' : '复制' }}
        </button>
      </div>
    </div>

    <p v-if="requestUrl" class="text-[11px] text-gray-500 font-mono break-all mb-3">{{ requestUrl }}</p>

    <div v-if="loading" class="flex items-center gap-2 text-sm text-gray-400 py-8 justify-center">
      <span class="w-4 h-4 border-2 border-suno-yellow border-t-transparent rounded-full animate-spin" />
      请求中…
    </div>

    <div v-else-if="error" class="rounded-lg border border-red-700 bg-red-900/20 text-red-300 text-sm p-3">
      {{ error }}
    </div>

    <template v-else-if="payload">
      <pre class="code max-h-[420px]"><code v-html="highlighted" /></pre>

      <div v-if="media.audio.length || media.video.length || media.image.length" class="mt-4 space-y-3">
        <p class="text-xs font-semibold text-suno-yellow">检测到媒体资源（链接 1 小时内有效，请及时转存）</p>

        <div v-for="url in media.audio" :key="url" class="space-y-1">
          <audio :src="url" controls preload="none" class="w-full" />
          <a :href="url" target="_blank" rel="noopener" class="text-[11px] text-gray-500 hover:text-suno-yellow break-all">
            {{ url }}
          </a>
        </div>

        <video v-for="url in media.video" :key="url" :src="url" controls preload="none" class="w-full rounded-lg" />

        <div v-if="media.image.length" class="flex flex-wrap gap-2">
          <img v-for="url in media.image" :key="url" :src="url" alt="封面" class="w-24 h-24 object-cover rounded-lg" />
        </div>
      </div>
    </template>

    <p v-else class="text-sm text-gray-500 py-8 text-center">发送请求后，响应内容会显示在这里</p>
  </div>
</template>
