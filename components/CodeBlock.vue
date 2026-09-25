<script setup lang="ts">
const props = defineProps<{ code: string; title?: string }>()

const copied = ref(false)
const copy = async () => {
  try {
    await navigator.clipboard.writeText(props.code)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {
    copied.value = false
  }
}
</script>

<template>
  <div class="rounded-lg border border-suno-gray-700 bg-suno-gray-900 overflow-hidden">
    <div
      v-if="title"
      class="flex items-center justify-between px-4 py-2 border-b border-suno-gray-700 bg-suno-gray-800"
    >
      <span class="text-xs font-medium text-gray-300">{{ title }}</span>
      <button class="text-xs text-gray-400 hover:text-suno-yellow transition-colors" @click="copy">
        {{ copied ? '已复制' : '复制' }}
      </button>
    </div>
    <pre class="p-4 overflow-x-auto font-mono text-[13px] leading-6 text-neutral-200 whitespace-pre">{{ code }}</pre>
  </div>
</template>
