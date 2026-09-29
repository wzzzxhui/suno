<script setup lang="ts">
useHead({ title: '关于我们 - SUNO API 开放平台' })

const config = useRuntimeConfig()

const features = [
  {
    icon: '💰',
    title: '统一服务',
    desc: '调用由平台账号统一提供，无需商户积分。'
  },
  {
    icon: '🛡️',
    title: '稳定可靠',
    desc: '7×24 监控，异常会在查询结果里如实返回；任务失败可重试。'
  },
  {
    icon: '📚',
    title: '文档完整',
    desc: '全部接口都有参数说明、响应示例与可直接运行的代码片段，并支持在线测试。'
  }
]

const steps = [
  { step: 1, title: '注册账号', desc: '点击右上角「登录后台」创建账户' },
  { step: 2, title: '获取 API Key', desc: '在后台「API 密钥管理」生成专属 access_key' },
  { step: 3, title: '开始调用', desc: '用在线测试工具跑通，再接入自己的应用' }
]

const stats = [
  { label: '开放接口', value: '19' },
  { label: '服务方式', value: '按需调用' },
  { label: '下载链接有效期', value: '1 小时' },
  { label: '建议轮询间隔', value: '3~5 秒' }
]
</script>

<template>
  <div class="py-12">
    <div class="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8">
      <header class="text-center mb-14">
        <h1 class="heading-lg mb-4">关于 SUNO API 平台</h1>
        <p class="text-gray-400 max-w-2xl mx-auto leading-7">
          面向开发者的 AI 音乐生成接口服务：生成、延长、翻唱、音效，以及 Remaster、MV、裁剪、变速等全套后期能力，
          都通过统一的异步任务接口提供。
        </p>
      </header>

      <!-- 数据条 -->
      <div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-14">
        <div v-for="item in stats" :key="item.label" class="card text-center">
          <p class="text-2xl font-extrabold text-suno-yellow font-mono">{{ item.value }}</p>
          <p class="text-xs text-gray-400 mt-1">{{ item.label }}</p>
        </div>
      </div>

      <!-- 优势 -->
      <section class="mb-14">
        <h2 class="heading-md text-center mb-6">为什么选择我们</h2>
        <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div v-for="f in features" :key="f.title" class="card">
            <div class="text-3xl mb-3">{{ f.icon }}</div>
            <h3 class="text-white font-semibold mb-2">{{ f.title }}</h3>
            <p class="text-sm text-gray-400 leading-6">{{ f.desc }}</p>
          </div>
        </div>
      </section>

      <!-- 快速开始 -->
      <section class="mb-14">
        <h2 class="heading-md text-center mb-6">快速开始</h2>
        <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div v-for="s in steps" :key="s.step" class="card flex gap-4">
            <div
              class="w-9 h-9 shrink-0 rounded-full bg-suno-yellow text-suno-bg font-bold
                     flex items-center justify-center"
            >
              {{ s.step }}
            </div>
            <div>
              <h3 class="text-white font-semibold mb-1">{{ s.title }}</h3>
              <p class="text-sm text-gray-400 leading-6">{{ s.desc }}</p>
            </div>
          </div>
        </div>
      </section>

      <!-- 联系 -->
      <section class="card text-center">
        <h2 class="heading-md mb-3">联系我们</h2>
        <p class="text-sm text-gray-400 mb-6 max-w-xl mx-auto leading-6">
          接入遇到问题，可以点右下角的在线客服。提问时附上接口名称、请求参数和返回的
          <code class="font-mono text-suno-yellow">task_id</code>，排查会快很多。
        </p>
        <div class="flex flex-wrap justify-center gap-3">
          <NuxtLink to="/" class="btn-primary">立即开始测试</NuxtLink>
          <NuxtLink to="/api-guide" class="btn-ghost">阅读使用指南</NuxtLink>
          <a :href="config.public.merchantUrl" target="_blank" rel="noopener" class="btn-ghost">登录后台</a>
        </div>
      </section>
    </div>
  </div>
</template>
