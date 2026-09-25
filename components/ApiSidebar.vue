<script setup lang="ts">
import type { ApiCategory } from '~/types/api'

const props = defineProps<{ categories: ApiCategory[]; selectedId: string }>()
const emit = defineEmits<{ select: [string] }>()

const keyword = ref('')
const collapsed = reactive<Record<string, boolean>>({})

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return props.categories
  return props.categories
    .map((cat) => ({
      ...cat,
      apis: cat.apis.filter(
        (api) =>
          api.name.toLowerCase().includes(kw) ||
          api.endpoint.toLowerCase().includes(kw) ||
          api.description.toLowerCase().includes(kw)
      )
    }))
    .filter((cat) => cat.apis.length > 0)
})

const toggle = (key: string) => (collapsed[key] = !collapsed[key])
</script>

<template>
  <div class="card sticky top-24">
    <h2 class="text-lg font-semibold text-white mb-4">API 列表</h2>
    <input v-model="keyword" type="text" placeholder="搜索 API…" class="input mb-4" />

    <div class="space-y-4 max-h-[calc(100vh-15rem)] overflow-y-auto pr-1">
      <div v-for="cat in filtered" :key="cat.key">
        <button
          class="text-sm font-semibold text-suno-yellow uppercase mb-2 flex items-center w-full text-left
                 hover:text-yellow-300 transition-colors"
          @click="toggle(cat.key)"
        >
          <svg
            class="w-4 h-4 mr-1 transition-transform flex-shrink-0"
            :class="collapsed[cat.key] ? '' : 'rotate-90'"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
          </svg>
          {{ cat.title }}
          <span class="ml-auto text-[11px] text-gray-500 font-normal">{{ cat.apis.length }}</span>
        </button>

        <div v-show="!collapsed[cat.key]" class="space-y-1">
          <button
            v-for="api in cat.apis"
            :key="api.id"
            class="w-full text-left px-3 py-2 rounded-md text-sm transition-colors"
            :class="
              selectedId === api.id
                ? 'bg-suno-yellow/15 text-suno-yellow font-medium border border-suno-yellow/40'
                : 'text-gray-300 hover:bg-suno-gray-700 border border-transparent'
            "
            @click="emit('select', api.id)"
          >
            {{ api.name }}
          </button>
        </div>
      </div>

      <p v-if="!filtered.length" class="text-sm text-gray-500 text-center py-6">没有匹配的接口</p>
    </div>
  </div>
</template>
