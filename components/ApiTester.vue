<script setup lang="ts">
import type { ApiDefinition } from '~/types/api'
import { toPrettyJson } from '~/utils/format'
import type { MusicTaskResult, ProxyResult } from '~/composables/useSunoApi'

const props = defineProps<{ api: ApiDefinition }>()

const config = useRuntimeConfig()
const { apiKey, hasKey } = useApiKey()
const { request, pollTask, normalizeStatus } = useSunoApi()

/* ---------------- 表单 ---------------- */

const values = reactive<Record<string, unknown>>({})

const resetValues = () => {
  for (const key of Object.keys(values)) delete values[key]
  for (const param of props.api.params) {
    values[param.name] = param.default ?? (param.type === 'boolean' ? false : '')
  }
}

watch(() => props.api.id, resetValues, { immediate: true })

const payload = computed(() => {
  const out: Record<string, unknown> = {}
  for (const param of props.api.params) {
    const v = values[param.name]
    if (v === undefined || v === null || v === '') continue
    out[param.name] = param.type === 'number' ? Number(v) : v
  }
  return out
})

const missingRequired = computed(() =>
  props.api.params.filter((p) => p.required && (payload.value[p.name] === undefined)).map((p) => p.name)
)

/* ---------------- 发送 ---------------- */

const loading = ref(false)
const result = ref<ProxyResult | null>(null)
const errorMessage = ref('')

const send = async () => {
  errorMessage.value = ''
  if (!hasKey.value) {
    errorMessage.value = '请先在右侧填写并保存 access_key'
    return
  }
  if (missingRequired.value.length) {
    errorMessage.value = `缺少必填参数：${missingRequired.value.join('、')}`
    return
  }

  loading.value = true
  result.value = null
  taskLogs.value = []
  try {
    result.value = await request(props.api.endpoint, props.api.method, payload.value)
    if (autoPoll.value) await startPolling()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '请求失败'
  } finally {
    loading.value = false
  }
}

/* ---------------- 轮询 ---------------- */

/** 会返回 task_id 的接口，支持自动轮询 */
const pollable = computed(() => props.api.method === 'POST' && props.api.category === 'music')
const autoPoll = ref(false)
const polling = ref(false)
const taskLogs = ref<Array<{ id: string; status: string; attempt: number; result?: MusicTaskResult }>>([])

/** 从响应中提取 task_id：支持 data:[id]、data:id、data.task_ids、data.task_id 多种结构 */
const extractTaskIds = (data: unknown): Array<number | string> => {
  if (data === null || data === undefined) return []
  if (typeof data === 'number' || (typeof data === 'string' && /^\d+$/.test(data))) return [data]
  if (Array.isArray(data)) return data.filter((v) => typeof v === 'number' || typeof v === 'string')
  if (typeof data === 'object') {
    const obj = data as Record<string, unknown>
    if (Array.isArray(obj.task_ids)) return obj.task_ids as Array<number | string>
    if (obj.task_id !== undefined) return [obj.task_id as number | string]
    if (obj.id !== undefined) return [obj.id as number | string]
  }
  return []
}

const taskIds = computed(() => extractTaskIds(result.value?.data?.data))

const startPolling = async () => {
  const ids = taskIds.value
  if (!ids.length) return

  polling.value = true
  taskLogs.value = ids.map((id) => ({ id: String(id), status: 'pending', attempt: 0 }))

  try {
    await Promise.all(
      ids.map(async (id, index) => {
        const final = await pollTask(id, {
          interval: 5000,
          maxAttempts: 60,
          onTick: (res, attempt) => {
            taskLogs.value[index] = {
              id: String(id),
              status: normalizeStatus(res.status),
              attempt,
              result: res
            }
          }
        })
        taskLogs.value[index] = {
          id: String(id),
          status: final.status,
          attempt: final.attempts,
          result: final.result
        }
      })
    )
  } finally {
    polling.value = false
  }
}

const statusTag = (status: string) => {
  if (status === 'completed') return 'bg-emerald-900/40 text-emerald-300 border-emerald-700'
  if (status === 'failed') return 'bg-red-900/40 text-red-300 border-red-700'
  if (status === 'processing') return 'bg-sky-900/40 text-sky-300 border-sky-700'
  return 'bg-suno-gray-700 text-gray-300 border-suno-gray-600'
}

const previewBody = computed(() => toPrettyJson(payload.value))
</script>

<template>
  <div class="space-y-4">
    <!-- 接口概览 -->
    <div class="card">
      <div class="flex flex-wrap items-center gap-2 mb-3">
        <span
          class="tag font-mono"
          :class="api.method === 'GET' ? 'bg-sky-900/40 text-sky-300 border-sky-700' : 'bg-emerald-900/40 text-emerald-300 border-emerald-700'"
        >
          {{ api.method }}
        </span>
        <code class="font-mono text-sm text-white">{{ api.endpoint }}</code>
        <span class="tag bg-suno-gray-700 text-gray-300 border-suno-gray-600 ml-auto">{{ api.provider }}</span>
      </div>

      <h2 class="heading-md mb-2">{{ api.name }}</h2>
      <p class="text-sm text-gray-400 leading-6">{{ api.description }}</p>

      <div class="flex flex-wrap gap-4 mt-4 pt-4 border-t border-suno-gray-700 text-sm">
        <div>
          <span class="text-gray-500">本站价格：</span>
          <span class="text-suno-yellow font-semibold font-mono">
            {{ api.pricing.our === 0 ? '免费' : `${api.pricing.our} ${api.pricing.unit}` }}
          </span>
        </div>
        <div v-if="api.pricing.official > 0">
          <span class="text-gray-500">官方参考：</span>
          <span class="text-gray-400 font-mono line-through">{{ api.pricing.official }} {{ api.pricing.unit }}</span>
        </div>
      </div>

      <ul v-if="api.notes?.length" class="mt-4 space-y-1.5">
        <li v-for="note in api.notes" :key="note" class="text-xs text-gray-400 flex gap-2 leading-5">
          <span class="text-suno-yellow">·</span>{{ note }}
        </li>
      </ul>
    </div>

    <!-- 参数表单 -->
    <div class="card">
      <div class="flex items-center justify-between mb-4">
        <h3 class="heading-md text-base">请求参数</h3>
        <button class="text-xs text-gray-400 hover:text-suno-yellow" @click="resetValues">重置</button>
      </div>

      <p v-if="!api.params.length" class="text-sm text-gray-500">该接口无需参数，直接发送即可。</p>

      <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <ParamField
          v-for="param in api.params"
          :key="param.name"
          v-model="values[param.name]"
          :param="param"
          :class="param.multiline || param.type === 'object' ? 'sm:col-span-2' : ''"
        />
      </div>

      <div class="mt-5 pt-4 border-t border-suno-gray-700">
        <p class="text-xs text-gray-500 mb-2">
          即将发送的{{ api.method === 'GET' ? 'Query 参数' : '请求体' }}
        </p>
        <pre class="code text-xs max-h-40">{{ previewBody }}</pre>
      </div>

      <div class="flex flex-wrap items-center gap-3 mt-4">
        <button class="btn-primary" :disabled="loading || polling" @click="send">
          <span v-if="loading" class="w-4 h-4 border-2 border-suno-bg border-t-transparent rounded-full animate-spin" />
          {{ loading ? '发送中…' : '发送请求' }}
        </button>

        <label v-if="pollable" class="flex items-center gap-2 text-xs text-gray-400 cursor-pointer">
          <input v-model="autoPoll" type="checkbox" class="accent-[#ffed29]" />
          发送后自动轮询任务结果
        </label>

        <span v-if="!hasKey" class="text-xs text-amber-400">未填写 access_key</span>
      </div>

      <p v-if="errorMessage" class="text-sm text-red-400 mt-3">{{ errorMessage }}</p>
    </div>

    <!-- 响应 -->
    <ResponsePanel
      :status="result?.status"
      :duration="result?.duration"
      :request-url="result?.requestUrl"
      :payload="result?.data"
      :loading="loading"
    />

    <!-- 轮询结果 -->
    <div v-if="taskIds.length && pollable" class="card">
      <div class="flex items-center justify-between mb-3">
        <h3 class="heading-md text-base">任务轮询</h3>
        <button class="btn-ghost text-xs py-1.5 px-3" :disabled="polling" @click="startPolling">
          {{ polling ? '轮询中…' : '开始轮询' }}
        </button>
      </div>

      <p class="text-xs text-gray-500 mb-3">
        共 {{ taskIds.length }} 个任务：
        <code class="font-mono text-suno-yellow">{{ taskIds.join('、') }}</code>
        ，每 5 秒查询一次，最多 60 次。
      </p>

      <div v-if="taskLogs.length" class="space-y-3">
        <div v-for="log in taskLogs" :key="log.id" class="rounded-lg border border-suno-gray-700 bg-suno-gray-900 p-3">
          <div class="flex items-center gap-2 mb-2">
            <code class="font-mono text-sm text-white">#{{ log.id }}</code>
            <span class="tag" :class="statusTag(log.status)">{{ log.status }}</span>
            <span class="text-[11px] text-gray-500 ml-auto">第 {{ log.attempt }} 次查询</span>
          </div>

          <template v-if="log.result?.custom_id">
            <p class="text-xs text-gray-400 break-all">
              custom_id：<code class="font-mono text-suno-yellow">{{ log.result.custom_id }}</code>
            </p>
          </template>

          <audio
            v-if="log.result?.fileInfo?.mp3Url"
            :src="log.result.fileInfo.mp3Url"
            controls
            preload="none"
            class="w-full mt-2"
          />
        </div>
      </div>
    </div>

    <!-- 代码示例 -->
    <CodeSamples :api="api" :values="values" :base-url="config.public.apiBase" />

    <!-- 响应示例与说明 -->
    <div v-if="api.responseExample || api.guide" class="card">
      <h3 class="heading-md text-base mb-3">响应说明</h3>

      <div v-if="api.responseExample">
        <p class="text-xs text-gray-500 mb-2">响应示例</p>
        <pre class="code text-xs mb-4">{{ toPrettyJson(api.responseExample) }}</pre>
      </div>

      <!-- eslint-disable-next-line vue/no-v-html -->
      <div v-if="api.guide" class="doc-body" v-html="api.guide" />
    </div>
  </div>
</template>
