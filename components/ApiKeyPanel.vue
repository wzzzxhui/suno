<script setup lang="ts">
const config = useRuntimeConfig()
const { apiKey, setApiKey, clearApiKey, hasKey } = useApiKey()
const { getBalance } = useSunoApi()

const draft = ref('')
const revealed = ref(false)
const balance = ref<number | null>(null)
const balanceError = ref('')
const loading = ref(false)

watchEffect(() => {
  if (!draft.value && apiKey.value) draft.value = apiKey.value
})

const save = () => {
  setApiKey(draft.value)
  balance.value = null
  balanceError.value = ''
}

const reset = () => {
  clearApiKey()
  draft.value = ''
  balance.value = null
  balanceError.value = ''
}

const checkBalance = async () => {
  if (!hasKey.value) {
    balanceError.value = '请先保存 access_key'
    return
  }
  loading.value = true
  balanceError.value = ''
  try {
    const res = await getBalance()
    if (res.ok && typeof res.data?.data?.remaining_points === 'number') {
      balance.value = res.data.data.remaining_points
    } else {
      balance.value = null
      balanceError.value = res.data?.message || `查询失败（HTTP ${res.status}）`
    }
  } catch (error) {
    balanceError.value = error instanceof Error ? error.message : '查询失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="space-y-4 sticky top-24">
    <div class="card">
      <h2 class="text-lg font-semibold text-white mb-1">鉴权</h2>
      <p class="text-xs text-gray-500 mb-3 leading-5">
        access_key 只保存在当前浏览器，测试请求由本站服务端转发，不会写入日志。
      </p>

      <label class="text-xs text-gray-400 font-mono">Authorization: Bearer …</label>
      <div class="relative mt-1.5">
        <input
          v-model="draft"
          :type="revealed ? 'text' : 'password'"
          class="input pr-14 font-mono text-xs"
          placeholder="粘贴你的 access_key"
          autocomplete="off"
          @keyup.enter="save"
        />
        <button
          class="absolute right-2 top-1/2 -translate-y-1/2 text-[11px] text-gray-400 hover:text-suno-yellow"
          type="button"
          @click="revealed = !revealed"
        >
          {{ revealed ? '隐藏' : '显示' }}
        </button>
      </div>

      <div class="flex gap-2 mt-3">
        <button class="btn-primary flex-1 text-sm py-2" @click="save">保存</button>
        <button class="btn-ghost text-sm py-2 px-3" @click="reset">清除</button>
      </div>

      <p v-if="hasKey" class="text-[11px] text-emerald-400 mt-2">✓ 已保存，可以开始测试</p>
      <p v-else class="text-[11px] text-gray-500 mt-2">
        还没有密钥？到
        <a :href="config.public.merchantUrl" target="_blank" rel="noopener" class="text-suno-yellow hover:underline">
          商户后台
        </a>
        的「API 密钥管理」创建。
      </p>
    </div>

    <div class="card">
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-lg font-semibold text-white">积分余额</h2>
        <button class="text-xs text-gray-400 hover:text-suno-yellow" :disabled="loading" @click="checkBalance">
          {{ loading ? '查询中…' : '刷新' }}
        </button>
      </div>

      <p v-if="balance !== null" class="text-3xl font-extrabold text-suno-yellow font-mono">
        {{ balance.toLocaleString() }}
      </p>
      <p v-else-if="balanceError" class="text-sm text-red-400">{{ balanceError }}</p>
      <p v-else class="text-sm text-gray-500">点击「刷新」查询当前余额</p>

      <p class="text-[11px] text-gray-500 mt-2">查询余额不消耗积分。</p>
    </div>

    <div class="card">
      <h2 class="text-lg font-semibold text-white mb-3">调用要点</h2>
      <ul class="space-y-2 text-xs text-gray-400 leading-5">
        <li>
          <span class="text-suno-yellow font-mono">task_id</span>
          是数字任务编号，用于查询任务状态。
        </li>
        <li>
          <span class="text-suno-yellow font-mono">custom_id</span>
          是 Suno 音乐 UUID，延长、翻唱、裁剪、变速等加工操作都用它。
        </li>
        <li>生成音乐一次返回两个 task_id，需要分别轮询。</li>
        <li>轮询间隔建议 3~5 秒，过密会触发 429 限流。</li>
        <li>音视频下载链接有效期 1 小时，请及时转存。</li>
      </ul>
    </div>
  </div>
</template>
