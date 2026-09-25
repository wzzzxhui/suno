<script setup lang="ts">
const config = useRuntimeConfig()
const year = new Date().getFullYear()
</script>

<template>
  <footer class="bg-suno-gray-800 border-t border-suno-gray-700 mt-auto">
    <div class="max-w-7xl mx-auto px-4 py-12 sm:px-6 lg:px-8">
      <div class="grid grid-cols-1 md:grid-cols-3 gap-8 mb-8">
        <div>
          <h3 class="text-white text-lg font-bold mb-4">SUNO API</h3>
          <p class="text-sm text-gray-400 leading-relaxed">
            SUNO AI 音乐生成 API 开放平台，提供稳定高效的音乐生成接口服务，覆盖生成、延长、翻唱与全部后期加工能力。
          </p>
        </div>

        <div>
          <h3 class="text-white text-lg font-bold mb-4">快速链接</h3>
          <ul class="space-y-2 text-sm text-gray-400">
            <li><NuxtLink to="/" class="hover:text-suno-yellow">API 文档与测试</NuxtLink></li>
            <li><NuxtLink to="/api-guide" class="hover:text-suno-yellow">API 使用指南</NuxtLink></li>
            <li><NuxtLink to="/verify" class="hover:text-suno-yellow">证书核验</NuxtLink></li>
            <li><NuxtLink to="/about" class="hover:text-suno-yellow">关于我们</NuxtLink></li>
            <li>
              <a :href="config.public.merchantUrl" target="_blank" rel="noopener" class="hover:text-suno-yellow">
                登录后台
              </a>
            </li>
          </ul>
        </div>

        <div>
          <h3 class="text-white text-lg font-bold mb-4">技术支持</h3>
          <p class="text-sm text-gray-400 mb-4">遇到问题？可通过页面右下角的在线客服入口联系我们。</p>
          <div class="text-sm text-gray-400 space-y-1">
            <p>接口基础地址：<code class="font-mono text-suno-yellow">/api/v1</code></p>
            <p>鉴权方式：<code class="font-mono text-suno-yellow">Bearer access_key</code></p>
          </div>
        </div>
      </div>

      <div class="border-t border-suno-gray-600 pt-8 text-center text-sm text-gray-500">
        © {{ year }} SUNO AI Music. All rights reserved.
      </div>
    </div>
  </footer>
</template>
