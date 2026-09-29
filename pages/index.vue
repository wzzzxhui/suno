<script setup lang="ts">
import { apiCategories, apiList, findApi } from '~/data/apis'

useHead({ title: 'API 文档与测试 - SUNO API 开放平台' })

const route = useRoute()
const router = useRouter()

const selectedId = ref<string>((route.query.api as string) || '')
const selected = computed(() => (selectedId.value ? findApi(selectedId.value) : undefined))

const select = (id: string) => {
  selectedId.value = id
  router.replace({ query: { ...route.query, api: id } })
}

watch(
  () => route.query.api,
  (id) => {
    if (typeof id === 'string' && id !== selectedId.value) selectedId.value = id
  }
)

const totalCount = apiList.length
</script>

<template>
  <div class="min-h-screen py-8">
    <div class="max-w-[1600px] mx-auto px-4 sm:px-6 lg:px-8">
      <!-- 页头 -->
      <div class="mb-6">
        <h1 class="heading-lg mb-2">SUNO API 在线测试</h1>
        <p class="text-gray-400">选择接口，填写参数，一键发起真实调用。共 {{ totalCount }} 个接口。</p>
        <div class="mt-3 flex flex-wrap gap-2">
          <span
            class="inline-flex items-center px-3 py-1 bg-emerald-900/30 text-emerald-400 rounded-full
                   text-sm font-medium border border-emerald-700"
          >
            由平台账号统一提供服务
          </span>
          <NuxtLink
            to="/api-guide"
            class="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium border
                   border-suno-gray-600 text-gray-300 hover:text-suno-yellow hover:border-suno-yellow transition-colors"
          >
            📘 先看使用指南
          </NuxtLink>
        </div>
      </div>

      <!-- 全局提示 -->
      <div
        role="alert"
        class="mb-6 rounded-lg px-4 py-2.5 sm:px-5 flex items-start sm:items-center gap-3 text-white shadow-sm"
        style="background: linear-gradient(to right, #b45309, #d97706)"
      >
        <svg class="w-5 h-5 mt-0.5 sm:mt-0 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
          />
        </svg>
        <p class="text-sm font-medium leading-5">
          受上游下载限制，音频与视频链接仅一小时内有效，拿到结果后请及时转存到自己的存储。
        </p>
      </div>

      <!-- 三栏布局 -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6">
        <div class="lg:col-span-3">
          <ApiSidebar :categories="apiCategories" :selected-id="selectedId" @select="select" />
        </div>

        <div class="lg:col-span-6">
          <ApiTester v-if="selected" :key="selected.id" :api="selected" />

          <div v-else class="card text-center py-16">
            <svg class="w-16 h-16 text-gray-600 mx-auto mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4"
              />
            </svg>
            <h3 class="text-lg font-medium text-white mb-2">请从左侧选择一个接口</h3>
            <p class="text-gray-400 mb-6">选择后可查看参数说明、代码示例，并直接发起测试</p>

            <div class="flex flex-wrap justify-center gap-2">
              <button
                v-for="api in apiList.slice(0, 6)"
                :key="api.id"
                class="px-3 py-1.5 text-xs rounded-md bg-suno-gray-700 text-gray-300
                       hover:bg-suno-gray-600 hover:text-suno-yellow transition-colors"
                @click="select(api.id)"
              >
                {{ api.name }}
              </button>
            </div>
          </div>
        </div>

        <div class="lg:col-span-3">
          <ApiKeyPanel />
        </div>
      </div>
    </div>
  </div>
</template>
