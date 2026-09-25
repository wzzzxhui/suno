<script setup lang="ts">
const open = ref(false)
</script>

<template>
  <div class="fixed bottom-6 right-6 z-50">
    <Transition
      enter-active-class="transition duration-150 ease-out"
      enter-from-class="opacity-0 translate-y-2"
      leave-active-class="transition duration-100 ease-in"
      leave-to-class="opacity-0 translate-y-2"
    >
      <div
        v-if="open"
        class="mb-3 w-72 card shadow-2xl border-suno-gray-600"
        role="dialog"
        aria-label="在线客服"
      >
        <h3 class="font-bold text-white mb-2">在线客服</h3>
        <p class="text-sm text-gray-400 leading-6">
          接入过程中遇到问题，可以带上接口名称、请求参数与返回的
          <code class="font-mono text-suno-yellow text-xs">task_id</code> 联系我们，排查会快很多。
        </p>
        <ul class="mt-3 space-y-1.5 text-sm text-gray-300">
          <li>· 接口报错 / 参数校验失败</li>
          <li>· 任务长时间未完成</li>
          <li>· 积分与账单问题</li>
        </ul>
        <NuxtLink to="/about" class="btn-ghost w-full mt-4 text-sm" @click="open = false">
          查看联系方式
        </NuxtLink>
      </div>
    </Transition>

    <button
      class="w-14 h-14 bg-suno-yellow hover:bg-yellow-300 text-suno-bg rounded-full shadow-lg
             flex items-center justify-center transition-all ml-auto"
      :title="open ? '收起' : '在线客服'"
      :aria-label="open ? '收起客服面板' : '打开在线客服'"
      @click="open = !open"
    >
      <svg v-if="!open" class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"
        />
      </svg>
      <svg v-else class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
      </svg>
    </button>
  </div>
</template>
