export default defineNuxtConfig({
  compatibilityDate: '2025-01-01',
  devtools: { enabled: false },
  modules: ['@nuxtjs/tailwindcss'],
  css: ['~/assets/css/main.css'],
  runtimeConfig: {
    // 在线测试时由 Nuxt 服务端转发到的后端地址，可用 NUXT_UPSTREAM_BASE 覆盖
    upstreamBase: 'http://127.0.0.1:8080',
    public: {
      // 代码示例里展示的接口地址，可用 NUXT_PUBLIC_API_BASE 覆盖
      apiBase: 'http://127.0.0.1:8080',
      merchantUrl: 'https://open.suno.cn/merchant/login'
    }
  },
  app: {
    head: {
      htmlAttrs: { lang: 'zh-CN' },
      title: 'SUNO API 开放平台',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'description', content: 'SUNO AI 音乐生成 API 开放平台：在线文档、参数说明与接口测试工具。' }
      ],
      link: [{ rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }]
    }
  }
})
