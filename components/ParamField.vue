<script setup lang="ts">
import type { ApiParam } from '~/types/api'

const props = defineProps<{ param: ApiParam; modelValue: unknown }>()
const emit = defineEmits<{ 'update:modelValue': [unknown] }>()

const value = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

const stringValue = computed({
  get: () => (value.value === undefined || value.value === null ? '' : String(value.value)),
  set: (v: string) => (value.value = v)
})

const numberValue = computed({
  get: () => (value.value === undefined || value.value === null || value.value === '' ? '' : Number(value.value)),
  set: (v: string | number) => (value.value = v === '' ? '' : Number(v))
})

const booleanValue = computed({
  get: () => value.value === true || value.value === 'true',
  set: (v: boolean) => (value.value = v)
})

/** object 类型参数用 JSON 文本框编辑 */
const jsonText = ref(
  props.param.type === 'object' && props.modelValue && typeof props.modelValue === 'object'
    ? JSON.stringify(props.modelValue, null, 2)
    : typeof props.modelValue === 'string'
      ? props.modelValue
      : ''
)
const jsonError = ref('')

watch(jsonText, (text) => {
  if (!text.trim()) {
    jsonError.value = ''
    value.value = undefined
    return
  }
  try {
    value.value = JSON.parse(text)
    jsonError.value = ''
  } catch {
    jsonError.value = 'JSON 格式有误'
  }
})
</script>

<template>
  <div>
    <label class="flex items-baseline justify-between gap-2 mb-1.5">
      <span class="text-sm font-mono text-white">
        {{ param.name }}
        <span v-if="param.required" class="text-red-400 ml-0.5">*</span>
      </span>
      <span class="text-[11px] text-gray-500 font-mono shrink-0">{{ param.type }}</span>
    </label>

    <!-- 枚举 -->
    <select v-if="param.options?.length" v-model="stringValue" class="input">
      <option value="">不传</option>
      <option v-for="opt in param.options" :key="opt" :value="opt">{{ opt }}</option>
    </select>

    <!-- 布尔 -->
    <div v-else-if="param.type === 'boolean'" class="flex items-center gap-3">
      <button
        type="button"
        class="relative w-11 h-6 rounded-full transition-colors"
        :class="booleanValue ? 'bg-suno-yellow' : 'bg-suno-gray-600'"
        role="switch"
        :aria-checked="booleanValue"
        @click="booleanValue = !booleanValue"
      >
        <span
          class="absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform"
          :class="booleanValue ? 'translate-x-5' : ''"
        />
      </button>
      <span class="text-sm font-mono text-gray-400">{{ booleanValue }}</span>
    </div>

    <!-- 对象（JSON） -->
    <div v-else-if="param.type === 'object'">
      <textarea v-model="jsonText" rows="5" class="input font-mono text-xs" placeholder="{ }" />
      <p v-if="jsonError" class="text-xs text-red-400 mt-1">{{ jsonError }}</p>
    </div>

    <!-- 数字 -->
    <input
      v-else-if="param.type === 'number'"
      v-model="numberValue"
      type="number"
      step="any"
      class="input"
      :placeholder="param.placeholder"
    />

    <!-- 多行文本 -->
    <textarea
      v-else-if="param.multiline"
      v-model="stringValue"
      rows="4"
      class="input"
      :placeholder="param.placeholder"
    />

    <!-- 单行文本 -->
    <input v-else v-model="stringValue" type="text" class="input" :placeholder="param.placeholder" />

    <p class="text-xs text-gray-500 mt-1.5 leading-5">{{ param.description }}</p>
  </div>
</template>
