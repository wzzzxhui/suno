<script setup lang="ts">
const config = useRuntimeConfig()
const mobileOpen = ref(false)
const route = useRoute()

const links = [
  { to: '/', label: 'API 文档与测试' },
  { to: '/api-guide', label: 'API 使用指南' },
  { to: '/verify', label: '证书核验' },
  { to: '/about', label: '关于我们' }
]

watch(() => route.fullPath, () => (mobileOpen.value = false))
</script>

<template>
  <nav class="sticky top-0 z-50 bg-suno-bg/90 backdrop-blur border-b border-suno-gray-800">
    <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div class="flex justify-between items-center h-20">
        <NuxtLink to="/" class="flex items-center space-x-3 group">
          <span
            class="text-3xl font-extrabold text-suno-yellow tracking-tight transition-all
                   group-hover:drop-shadow-[0_0_8px_rgba(255,237,41,0.5)]"
          >
            SUNO
          </span>
          <span
            class="text-xs px-2 py-0.5 rounded border border-suno-gray-600 bg-suno-gray-800
                   text-suno-text-light hidden sm:inline-block"
          >
            AI 音乐创作
          </span>
        </NuxtLink>

        <div class="hidden md:flex items-center space-x-8">
          <NuxtLink
            v-for="link in links"
            :key="link.to"
            :to="link.to"
            class="text-sm font-medium transition-colors hover:text-suno-yellow"
            active-class="text-suno-yellow"
            exact-active-class="text-suno-yellow"
          >
            {{ link.label }}
          </NuxtLink>
          <a :href="config.public.merchantUrl" target="_blank" rel="noopener" class="btn-primary text-sm py-2 px-5">
            登录后台
          </a>
        </div>

        <button
          class="md:hidden p-2 text-suno-text-light hover:text-suno-yellow"
          :aria-expanded="mobileOpen"
          aria-label="切换导航"
          @click="mobileOpen = !mobileOpen"
        >
          <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              :d="mobileOpen ? 'M6 18L18 6M6 6l12 12' : 'M4 6h16M4 12h16M4 18h16'"
            />
          </svg>
        </button>
      </div>
    </div>

    <div v-if="mobileOpen" class="md:hidden border-t border-suno-gray-800 bg-suno-bg px-4 py-4 space-y-3">
      <NuxtLink
        v-for="link in links"
        :key="link.to"
        :to="link.to"
        class="block text-sm font-medium py-1.5 hover:text-suno-yellow"
        active-class="text-suno-yellow"
      >
        {{ link.label }}
      </NuxtLink>
      <a :href="config.public.merchantUrl" target="_blank" rel="noopener" class="btn-primary w-full text-sm">
        登录后台
      </a>
    </div>
  </nav>
</template>
