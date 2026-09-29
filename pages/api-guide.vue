<script setup lang="ts">
useHead({ title: 'API 使用指南 - SUNO API 开放平台' })

const toc = [
  { id: 'before', label: '开始之前' },
  { id: 'flow', label: '核心调用流程' },
  { id: 'generate', label: '生成音乐' },
  { id: 'sound', label: '生成音效' },
  { id: 'query', label: '查询任务' },
  { id: 'upload', label: '上传参考音频' },
  { id: 'post', label: '后期处理' },
  { id: 'points', label: '历史积分记录' },
  { id: 'errors', label: '错误码' },
  { id: 'faq', label: '常见问题' },
  { id: 'example', label: '完整代码示例' }
]

const models = [
  { code: 'chirp-hawk', label: 'Suno V6', desc: '默认推荐，综合质量最稳' },
  { code: 'chirp-hawk-wild', label: 'Suno V6-wild', desc: '风格更外放、更实验' },
  { code: 'chirp-goose', label: 'Suno V6-mini', desc: '更快，适合批量试听' },
  { code: 'chirp-fenix', label: 'Suno V5.5', desc: '旧版本，仍可调用' },
  { code: 'chirp-crow', label: 'Suno V5', desc: '旧版本，音效生成也用它' },
  { code: 'chirp-bluejay', label: 'Suno V4.5+', desc: '旧版本' },
  { code: 'chirp-auk', label: 'Suno V4.5', desc: '旧版本' },
  { code: 'chirp-v4', label: 'Suno V4', desc: '旧版本' },
  { code: 'chirp-v3-5 / chirp-v3-0', label: 'Suno V3.5 / V3', desc: '已下线，自动定向到较新版本' }
]

const inspirationSnippet = `POST /api/v1/music/generate

{
  "gpt_description_prompt": "一首欢快的流行歌，关于夏天的美好回忆",
  "make_instrumental": false,
  "mv": "chirp-hawk",
  "title": "夏日回忆"
}`

const customSnippet = `POST /api/v1/music/generate

{
  "prompt": "[Verse]\\n第一段歌词\\n[Chorus]\\n副歌歌词",
  "tags": "pop, acoustic, female vocals",
  "make_instrumental": false,
  "mv": "chirp-hawk",
  "title": "青春的模样"
}`

const extendSnippet = `POST /api/v1/music/generate

{
  "task": "extend",
  "continue_clip_id": "上一首歌的 custom_id",
  "continue_at": 120,
  "prompt": "[Verse 3]\\n续写部分的歌词",
  "mv": "chirp-hawk",
  "title": "夏日回忆（续）"
}`

const coverSnippet = `POST /api/v1/music/generate

{
  "task": "cover",
  "cover_clip_id": "要翻唱的 custom_id",
  "prompt": "翻唱使用的歌词，可沿用原曲歌词",
  "tags": "jazz, piano, male vocals",
  "mv": "chirp-hawk",
  "title": "夏日回忆（爵士版）"
}`

const soundSnippet = `POST /api/v1/music/sound

{
  "title": "Rain",
  "tags": "rain",
  "mv": "chirp-crow",
  "tempo": 99,
  "key": "D",
  "loop": true
}`

const querySnippet = `GET /api/v1/music/task?id=199824

GET /api/v1/music/tasks?ids=199824,199825&page=1&size=20`

const uploadSnippet = `POST /api/v1/music/upload

{
  "audio_url": "https://example.com/my-song.mp3",
  "copyrightAudio": false
}`

const pointsSnippet = `GET /api/v1/points/balance
→ { "data": { "remaining_points": 1500 } }

GET /api/v1/points/logs?page=1&limit=20
→ type: 1=消耗  2=充值  3=手动调整  4=退还`

const fullExample = `const BASE = 'https://open.suno.cn'
const KEY = process.env.SUNO_ACCESS_KEY

const headers = {
  Authorization: \`Bearer \${KEY}\`,
  'Content-Type': 'application/json'
}

// 1. 提交生成任务
const submit = await fetch(\`\${BASE}/api/v1/music/generate\`, {
  method: 'POST',
  headers,
  body: JSON.stringify({
    gpt_description_prompt: '一首轻快的夏日流行曲',
    make_instrumental: false,
    mv: 'chirp-hawk',
    title: '夏日微风'
  })
}).then(r => r.json())

const taskIds = Array.isArray(submit.data) ? submit.data : submit.data.task_ids

// 2. 轮询直到完成
async function waitTask(id) {
  for (let i = 0; i < 60; i++) {
    await new Promise(r => setTimeout(r, 5000))
    const res = await fetch(\`\${BASE}/api/v1/music/task?id=\${id}\`, { headers })
      .then(r => r.json())
    const task = res.data
    const done = task.status === 'completed' || task.status === 3
    const failed = task.status === 'failed' || task.status === 4
    if (done) return task
    // 生成失败可调 /api/v1/music/retry 重试（任务 ID 不变）
    if (failed) throw new Error('任务失败，可重试')
  }
  throw new Error('轮询超时')
}

// 3. 取结果：custom_id 用于后续加工，mp3Url 是音频地址
const songs = await Promise.all(taskIds.map(waitTask))
for (const song of songs) {
  console.log(song.custom_id, song.fileInfo?.mp3Url)
}`

const postProcess = [
  { name: '合成整首歌', endpoint: 'POST /api/v1/music/whole-song', body: '{ "clip_id": "custom_id" }', desc: '把多段续写片段合并成完整歌曲' },
  { name: '歌词时间戳', endpoint: 'POST /api/v1/music/aligned-lyrics', body: '{ "lyrics": "…", "suno_id": "custom_id" }', desc: '逐字对齐，做字幕或卡拉 OK' },
  { name: 'Remaster', endpoint: 'POST /api/v1/music/upsample', body: '{ "clip_id": "custom_id", "model_name": "chirp-carp" }', desc: '升采样提升音质，返回两个任务' },
  { name: '生成 MV', endpoint: 'POST /api/v1/music/video', body: '{ "task_id": 199824, "suno_id": "custom_id" }', desc: '结果在 extend.video_url，优先用 proxy_url' },
  { name: '格式转换', endpoint: 'POST /api/v2/music/download-wav | -mp3 | -m4a', body: '{ "suno_id": "custom_id" }', desc: '注意是 v2 路径' },
  { name: '裁剪', endpoint: 'POST /api/v1/music/crop', body: '{ "clip_id": "custom_id", "crop_start_s": 10, "crop_end_s": 60 }', desc: '结束时间必须大于开始时间' },
  { name: '变速', endpoint: 'POST /api/v1/music/speed', body: '{ "clip_id": "custom_id", "speed_multiplier": 1.5, "keep_pitch": true }', desc: '倍数限 0.25 / 0.5 / 0.75 / 1 / 1.25 / 1.5 / 2' }
]

const errors = [
  { code: 200, meaning: '成功', action: '正常读取 data' },
  { code: 400, meaning: '参数错误', action: '检查必填项与取值范围' },
  { code: 401, meaning: '鉴权失败', action: '确认 Authorization 头与 access_key' },

  { code: 429, meaning: '触发限流', action: '降低请求频率，轮询间隔拉到 5 秒以上' },
  { code: 500, meaning: '服务端异常', action: '稍后重试；持续出现请联系客服' }
]

const faqs = [
  {
    q: 'task_id 和 custom_id 有什么区别？',
    a: 'task_id 是平台的任务编号（数字），只用来查询任务状态；custom_id 是 Suno 的音乐 ID（UUID 字符串）。延长、翻唱、合成整首歌、裁剪、变速等所有加工操作都要用 custom_id。'
  },
  {
    q: '为什么生成音乐返回两个 task_id？',
    a: '一次生成会产出两个版本，所以返回两个任务编号，需要分别轮询查询。'
  },
  {
    q: 'extend 字段怎么用？',
    a: 'extend 是 JSON 字符串，需要先 JSON.parse。解析后是歌曲完整信息，用其中的 id 与外层 custom_id 对应，即可定位到当前这首歌的标题、歌词、风格等数据。'
  },
  {
    q: '任务失败怎么办？',
    a: '所有服务无需商户积分。生成失败后可调用 POST /api/v1/music/retry 传入任务 id，任务 id 不变。每个任务最多重试 3 次。'
  },
  {
    q: '下载链接失效了怎么办？',
    a: '音视频链接有效期为一小时。重新调用查询任务接口可以拿到新的地址；生产环境建议拿到后立刻转存到自己的对象存储。'
  }
]
</script>

<template>
  <div class="py-10">
    <div class="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8">
      <header class="mb-10">
        <h1 class="heading-lg mb-3">API 使用指南</h1>
        <p class="text-gray-400">从拿到密钥到跑通第一首歌，这一页就够了。</p>
      </header>

      <!-- 目录 -->
      <nav class="card mb-10">
        <h2 class="text-sm font-semibold text-suno-yellow uppercase mb-3">目录</h2>
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-2">
          <a
            v-for="item in toc"
            :key="item.id"
            :href="`#${item.id}`"
            class="text-sm text-gray-300 hover:text-suno-yellow transition-colors py-1"
          >
            {{ item.label }}
          </a>
        </div>
      </nav>

      <!-- 开始之前 -->
      <section id="before" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-4">开始之前</h2>
        <div class="card space-y-4">
          <ol class="space-y-2 text-sm text-gray-300 list-decimal pl-5">
            <li>注册并登录商户后台。</li>
            <li>进入「API 密钥管理」，创建一条新密钥，拿到 <code class="font-mono text-suno-yellow">access_key</code>。</li>
            <li>每次请求都带上下面两个 Header。</li>
          </ol>
          <CodeBlock
            title="请求头"
            code="Authorization: Bearer 你的access_key
Content-Type: application/json"
          />
          <p class="text-sm text-gray-400">
            基础地址：<code class="font-mono text-suno-yellow">/api/v1</code>；
            三个格式下载接口在 <code class="font-mono text-suno-yellow">/api/v2</code> 下。
          </p>
        </div>
      </section>

      <!-- 核心流程 -->
      <section id="flow" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-4">核心调用流程</h2>
        <div class="card">
          <p class="text-sm text-gray-400 mb-4">几乎所有生成类接口都是异步的，流程完全一致：</p>

          <div class="flex flex-col sm:flex-row items-stretch gap-3 mb-5">
            <div class="flex-1 rounded-lg border border-suno-gray-600 bg-suno-gray-900 p-4 text-center">
              <div class="text-suno-yellow font-bold text-lg mb-1">1</div>
              <p class="text-sm text-white font-medium">提交任务</p>
              <p class="text-xs text-gray-500 mt-1">POST 生成 / 加工接口</p>
            </div>
            <div class="hidden sm:flex items-center text-gray-600">→</div>
            <div class="flex-1 rounded-lg border border-suno-gray-600 bg-suno-gray-900 p-4 text-center">
              <div class="text-suno-yellow font-bold text-lg mb-1">2</div>
              <p class="text-sm text-white font-medium">拿到 task_id</p>
              <p class="text-xs text-gray-500 mt-1">数字任务编号</p>
            </div>
            <div class="hidden sm:flex items-center text-gray-600">→</div>
            <div class="flex-1 rounded-lg border border-suno-gray-600 bg-suno-gray-900 p-4 text-center">
              <div class="text-suno-yellow font-bold text-lg mb-1">3</div>
              <p class="text-sm text-white font-medium">轮询查结果</p>
              <p class="text-xs text-gray-500 mt-1">间隔 3~5 秒</p>
            </div>
          </div>

          <div class="rounded-lg border border-amber-700 bg-amber-900/20 p-4">
            <p class="text-sm text-amber-200 leading-6">
              <strong>关键区分：</strong>
              <code class="font-mono">task_id</code> 是任务编号（数字），只用于查状态；
              <code class="font-mono">custom_id</code> 是 Suno 音乐 ID（UUID），后续所有加工操作都用它。
            </p>
          </div>
        </div>
      </section>

      <!-- 生成音乐 -->
      <section id="generate" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-1">生成音乐</h2>
        <p class="text-sm text-gray-500 font-mono mb-4">POST /api/v1/music/generate</p>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
          <div class="card">
            <h3 class="text-white font-semibold mb-1">🎤 灵感模式</h3>
            <p class="text-xs text-gray-400 mb-3">给一句描述，模型自由发挥，最省事的用法。</p>
            <CodeBlock :code="inspirationSnippet" />
          </div>
          <div class="card">
            <h3 class="text-white font-semibold mb-1">📝 自定义歌词</h3>
            <p class="text-xs text-gray-400 mb-3">自己写歌词与风格标签，精确控制结果。</p>
            <CodeBlock :code="customSnippet" />
          </div>
          <div class="card">
            <h3 class="text-white font-semibold mb-1">🔄 延长模式</h3>
            <p class="text-xs text-gray-400 mb-3">把一首歌从指定秒数继续写下去。</p>
            <CodeBlock :code="extendSnippet" />
          </div>
          <div class="card">
            <h3 class="text-white font-semibold mb-1">🎙️ 翻唱模式</h3>
            <p class="text-xs text-gray-400 mb-3">换一套风格重唱已有歌曲。</p>
            <CodeBlock :code="coverSnippet" />
          </div>
        </div>

        <div class="card">
          <h3 class="text-white font-semibold mb-3">模型版本</h3>
          <div class="space-y-2">
            <div v-for="m in models" :key="m.code" class="flex flex-wrap items-baseline gap-2 text-sm">
              <code class="font-mono text-suno-yellow">{{ m.code }}</code>
              <span class="text-white">{{ m.label }}</span>
              <span class="text-gray-500 text-xs">{{ m.desc }}</span>
            </div>
          </div>
          <p class="text-xs text-gray-500 mt-3">
            Remaster 用的是另一套代号：chirp-halibut(V6)、chirp-flounder(V5.5)、chirp-carp(V5)、
            chirp-bass(V4.5)、chirp-up(V4)，需与原歌曲版本匹配。
          </p>
        </div>
      </section>

      <!-- 生成音效 -->
      <section id="sound" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-1">生成音效</h2>
        <p class="text-sm text-gray-500 font-mono mb-4">POST /api/v1/music/sound</p>
        <div class="card space-y-3">
          <CodeBlock :code="soundSnippet" />
          <ul class="text-sm text-gray-400 space-y-1.5 leading-6">
            <li>· 模型仅支持 <code class="font-mono text-suno-yellow">chirp-crow</code> 与 <code class="font-mono text-suno-yellow">chirp-fenix</code>。</li>
            <li>· 服务端会自动固定 task=sound、generation_type=TEXT、make_instrumental=true。</li>
            <li>· tempo / key / loop 会映射到上游的 metadata.sound_configs。</li>
            <li>· 查询方式与生成音乐一致。</li>
          </ul>
        </div>
      </section>

      <!-- 查询任务 -->
      <section id="query" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-1">查询任务</h2>
        <p class="text-sm text-gray-500 font-mono mb-4">GET /api/v1/music/task · GET /api/v1/music/tasks</p>
        <div class="card space-y-4">
          <CodeBlock :code="querySnippet" />
          <div>
            <p class="text-sm text-white font-medium mb-2">响应里最该关注的字段</p>
            <div class="overflow-x-auto">
              <table class="w-full text-xs border-collapse">
                <thead>
                  <tr>
                    <th class="border border-suno-gray-600 bg-suno-gray-900 px-2.5 py-1.5 text-left text-gray-200">字段</th>
                    <th class="border border-suno-gray-600 bg-suno-gray-900 px-2.5 py-1.5 text-left text-gray-200">说明</th>
                  </tr>
                </thead>
                <tbody class="text-gray-300">
                  <tr><td class="border border-suno-gray-600 px-2.5 py-1.5 font-mono text-suno-yellow">result.custom_id</td><td class="border border-suno-gray-600 px-2.5 py-1.5">Suno 音乐 ID，后续加工全靠它</td></tr>
                  <tr><td class="border border-suno-gray-600 px-2.5 py-1.5 font-mono text-suno-yellow">result.fileInfo.mp3Url</td><td class="border border-suno-gray-600 px-2.5 py-1.5">音频地址，1 小时有效</td></tr>
                  <tr><td class="border border-suno-gray-600 px-2.5 py-1.5 font-mono text-suno-yellow">result.fileInfo.coverUrl</td><td class="border border-suno-gray-600 px-2.5 py-1.5">封面图地址</td></tr>
                  <tr><td class="border border-suno-gray-600 px-2.5 py-1.5 font-mono text-suno-yellow">result.fileInfo.duration</td><td class="border border-suno-gray-600 px-2.5 py-1.5">时长（秒）</td></tr>
                  <tr><td class="border border-suno-gray-600 px-2.5 py-1.5 font-mono text-suno-yellow">result.extend</td><td class="border border-suno-gray-600 px-2.5 py-1.5">JSON 字符串，需 JSON.parse 后使用</td></tr>
                  <tr><td class="border border-suno-gray-600 px-2.5 py-1.5 font-mono text-suno-yellow">points_refunded</td><td class="border border-suno-gray-600 px-2.5 py-1.5">历史退款状态（当前服务无需商户积分）</td></tr>
                  <tr><td class="border border-suno-gray-600 px-2.5 py-1.5 font-mono text-suno-yellow">retry_count</td><td class="border border-suno-gray-600 px-2.5 py-1.5">已重试的次数</td></tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </section>

      <!-- 上传参考音频 -->
      <section id="upload" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-1">上传参考音频</h2>
        <p class="text-sm text-gray-500 font-mono mb-4">POST /api/v1/music/upload</p>
        <div class="card space-y-3">
          <CodeBlock :code="uploadSnippet" />
          <p class="text-sm text-gray-400 leading-6">
            提交后拿 task_id 轮询，完成后从 <code class="font-mono text-suno-yellow">result.custom_id</code>
            取得音乐 ID，就能用在延长或翻唱模式里。音频 URL 必须可公开访问；
            <code class="font-mono text-suno-yellow">copyrightAudio</code> 传 true 不额外收费。
          </p>
        </div>
      </section>

      <!-- 后期处理 -->
      <section id="post" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-4">后期处理</h2>
        <p class="text-sm text-gray-400 mb-4">全部是异步接口，流程与生成音乐一样：提交 → 拿 task_id → 轮询。</p>
        <div class="space-y-3">
          <div v-for="item in postProcess" :key="item.name" class="card">
            <div class="flex flex-wrap items-baseline gap-2 mb-2">
              <h3 class="text-white font-semibold">{{ item.name }}</h3>
              <code class="font-mono text-xs text-gray-400">{{ item.endpoint }}</code>
            </div>
            <CodeBlock :code="item.body" />
            <p class="text-xs text-gray-500 mt-2">{{ item.desc }}</p>
          </div>
        </div>
      </section>

      <!-- 积分 -->
      <section id="points" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-4">历史积分记录</h2>
        <div class="card">
          <CodeBlock :code="pointsSnippet" />
          <p class="text-xs text-gray-500 mt-3">服务调用无需商户积分，余额与流水仅保留历史记录；任务失败可调用重试接口。</p>
        </div>
      </section>

      <!-- 错误码 -->
      <section id="errors" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-4">错误码</h2>
        <div class="card overflow-x-auto">
          <table class="w-full text-sm border-collapse">
            <thead>
              <tr>
                <th class="border border-suno-gray-600 bg-suno-gray-900 px-3 py-2 text-left text-gray-200">状态码</th>
                <th class="border border-suno-gray-600 bg-suno-gray-900 px-3 py-2 text-left text-gray-200">含义</th>
                <th class="border border-suno-gray-600 bg-suno-gray-900 px-3 py-2 text-left text-gray-200">处理建议</th>
              </tr>
            </thead>
            <tbody class="text-gray-300">
              <tr v-for="e in errors" :key="e.code">
                <td class="border border-suno-gray-600 px-3 py-2 font-mono text-suno-yellow">{{ e.code }}</td>
                <td class="border border-suno-gray-600 px-3 py-2">{{ e.meaning }}</td>
                <td class="border border-suno-gray-600 px-3 py-2 text-gray-400">{{ e.action }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- FAQ -->
      <section id="faq" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-4">常见问题</h2>
        <div class="space-y-3">
          <details v-for="item in faqs" :key="item.q" class="card group">
            <summary class="cursor-pointer text-white font-medium list-none flex items-center justify-between gap-3">
              {{ item.q }}
              <span class="text-suno-yellow transition-transform group-open:rotate-45">＋</span>
            </summary>
            <p class="text-sm text-gray-400 leading-6 mt-3">{{ item.a }}</p>
          </details>
        </div>
      </section>

      <!-- 完整示例 -->
      <section id="example" class="mb-12 scroll-mt-24">
        <h2 class="heading-md mb-4">完整代码示例</h2>
        <CodeBlock title="生成音乐 + 轮询取结果（JavaScript）" :code="fullExample" />
        <NuxtLink to="/" class="btn-primary mt-6">去在线测试</NuxtLink>
      </section>
    </div>
  </div>
</template>
