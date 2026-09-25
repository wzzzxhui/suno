<script setup lang="ts">
import type { ApiDefinition } from '~/types/api'
import { toPrettyJson } from '~/utils/format'

const props = defineProps<{ api: ApiDefinition; values: Record<string, unknown>; baseUrl: string }>()

type Lang = 'curl' | 'js' | 'python'
const lang = ref<Lang>('curl')
const tabs: Array<{ key: Lang; label: string }> = [
  { key: 'curl', label: 'cURL' },
  { key: 'js', label: 'JavaScript' },
  { key: 'python', label: 'Python' }
]

/** 过滤掉空值，得到真正会发送的参数 */
const effective = computed(() => {
  const out: Record<string, unknown> = {}
  for (const param of props.api.params) {
    const v = props.values[param.name]
    if (v === undefined || v === null || v === '') continue
    out[param.name] = v
  }
  return out
})

const queryString = computed(() => {
  const entries = Object.entries(effective.value).map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
  return entries.length ? `?${entries.join('&')}` : ''
})

const fullUrl = computed(
  () => `${props.baseUrl}${props.api.endpoint}${props.api.method === 'GET' ? queryString.value : ''}`
)

const bodyJson = computed(() => toPrettyJson(effective.value))

const snippet = computed(() => {
  const url = fullUrl.value
  if (lang.value === 'curl') {
    if (props.api.method === 'GET') {
      return [`curl -X GET "${url}" \\`, `  -H "Authorization: Bearer $ACCESS_KEY"`].join('\n')
    }
    return [
      `curl -X POST "${url}" \\`,
      `  -H "Authorization: Bearer $ACCESS_KEY" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${JSON.stringify(effective.value)}'`
    ].join('\n')
  }

  if (lang.value === 'js') {
    if (props.api.method === 'GET') {
      return [
        `const res = await fetch('${url}', {`,
        `  headers: { Authorization: \`Bearer \${ACCESS_KEY}\` }`,
        `})`,
        `const { data } = await res.json()`,
        `console.log(data)`
      ].join('\n')
    }
    return [
      `const res = await fetch('${url}', {`,
      `  method: 'POST',`,
      `  headers: {`,
      `    Authorization: \`Bearer \${ACCESS_KEY}\`,`,
      `    'Content-Type': 'application/json'`,
      `  },`,
      `  body: JSON.stringify(${bodyJson.value.replace(/\n/g, '\n  ')})`,
      `})`,
      `const { data } = await res.json()`,
      `console.log(data)`
    ].join('\n')
  }

  // python
  if (props.api.method === 'GET') {
    return [
      `import requests`,
      ``,
      `res = requests.get(`,
      `    "${props.baseUrl}${props.api.endpoint}",`,
      `    params=${toPythonLiteral(effective.value)},`,
      `    headers={"Authorization": f"Bearer {ACCESS_KEY}"},`,
      `    timeout=30,`,
      `)`,
      `print(res.json())`
    ].join('\n')
  }
  return [
    `import requests`,
    ``,
    `res = requests.post(`,
    `    "${props.baseUrl}${props.api.endpoint}",`,
    `    json=${toPythonLiteral(effective.value)},`,
    `    headers={`,
    `        "Authorization": f"Bearer {ACCESS_KEY}",`,
    `        "Content-Type": "application/json",`,
    `    },`,
    `    timeout=60,`,
    `)`,
    `print(res.json())`
  ].join('\n')
})

function toPythonLiteral(value: unknown): string {
  return toPrettyJson(value)
    .replace(/\btrue\b/g, 'True')
    .replace(/\bfalse\b/g, 'False')
    .replace(/\bnull\b/g, 'None')
    .replace(/\n/g, '\n    ')
}

const copied = ref(false)
const copy = async () => {
  try {
    await navigator.clipboard.writeText(snippet.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {
    copied.value = false
  }
}
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between gap-3 mb-3">
      <h3 class="heading-md text-base">请求示例</h3>
      <button class="text-xs text-gray-400 hover:text-suno-yellow transition-colors" @click="copy">
        {{ copied ? '已复制' : '复制代码' }}
      </button>
    </div>

    <div class="flex gap-1 mb-3">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="px-3 py-1.5 text-xs rounded-md transition-colors"
        :class="lang === tab.key ? 'bg-suno-yellow text-suno-bg font-semibold' : 'bg-suno-gray-700 text-gray-300 hover:text-white'"
        @click="lang = tab.key"
      >
        {{ tab.label }}
      </button>
    </div>

    <pre class="code whitespace-pre">{{ snippet }}</pre>
  </div>
</template>
