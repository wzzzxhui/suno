const STORAGE_KEY = 'suno_access_key'

const apiKey = ref('')
let restored = false

/**
 * access_key 的本地存取。只保存在浏览器 localStorage，不会随页面上报到任何第三方。
 */
export function useApiKey() {
  if (import.meta.client && !restored) {
    restored = true
    try {
      apiKey.value = localStorage.getItem(STORAGE_KEY) || ''
    } catch {
      // 隐私模式下 localStorage 可能不可用，忽略即可
    }
  }

  const setApiKey = (value: string) => {
    apiKey.value = value.trim()
    if (!import.meta.client) return
    try {
      if (apiKey.value) localStorage.setItem(STORAGE_KEY, apiKey.value)
      else localStorage.removeItem(STORAGE_KEY)
    } catch {
      // 同上
    }
  }

  const clearApiKey = () => setApiKey('')

  const maskedKey = computed(() => {
    const k = apiKey.value
    if (!k) return ''
    if (k.length <= 8) return `${k.slice(0, 2)}****`
    return `${k.slice(0, 4)}****${k.slice(-4)}`
  })

  return { apiKey, setApiKey, clearApiKey, maskedKey, hasKey: computed(() => !!apiKey.value) }
}
