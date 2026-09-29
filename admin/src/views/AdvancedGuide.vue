<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { guideTools, guideToolByKey } from '@/data/advancedGuide'

const route = useRoute()
const topic = computed(() => String(route.query.tool || 'overview'))
const selected = computed(() => guideToolByKey[topic.value])
const example = computed(() => selected.value?.example ? JSON.stringify(selected.value.example, null, 2) : '')
const docsUrl = computed(() => selected.value?.path ? 'https://platform.mureka.cn/docs/api/operations/' + selected.value.path + '.html' : '')

async function copyExample() {
  try {
    await navigator.clipboard.writeText(example.value)
    ElMessage.success('示例已复制，请替换示例中的 ID')
  } catch {
    ElMessage.error('复制失败，请手动选择示例文本')
  }
}
</script>

<template>
  <div class="page guide-page">
    <div class="page-header">
      <div>
        <h2>高级功能教程</h2>
        <p class="desc">选择功能，查看参数格式、必填字段和填写示例。</p>
      </div>
      <router-link to="/studio">返回音乐创作</router-link>
    </div>

    <el-row :gutter="16">
      <el-col :xs="24" :lg="7">
        <el-card shadow="never" class="guide-nav">
          <div class="guide-nav-title">入门</div>
          <router-link :class="{ active: topic === 'overview' }" :to="{ path: '/guide', query: { tool: 'overview' } }">JSON 语法与 ID</router-link>
          <router-link :class="{ active: topic === 'cover_advanced' }" :to="{ path: '/guide', query: { tool: 'cover_advanced' } }">高级模式参考重唱</router-link>
          <router-link :class="{ active: topic === 'creation' }" :to="{ path: '/guide', query: { tool: 'creation' } }">高级模式创作参数</router-link>
          <router-link :class="{ active: topic === 'metadata' }" :to="{ path: '/guide', query: { tool: 'metadata' } }">普通模式高级参数</router-link>
          <router-link :class="{ active: topic === 'upload' }" :to="{ path: '/guide', query: { tool: 'upload' } }">素材上传</router-link>
          <router-link :class="{ active: topic === 'upload_part' }" :to="{ path: '/guide', query: { tool: 'upload_part' } }">大文件分块上传</router-link>
          <div class="guide-nav-title">高级工具</div>
          <router-link v-for="tool in guideTools" :key="tool.key" :class="{ active: topic === tool.key }" :to="{ path: '/guide', query: { tool: tool.key } }">{{ tool.label }}</router-link>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="17">
        <el-card v-if="selected" shadow="never">
          <template #header><strong>{{ selected.label }}</strong></template>
          <p>{{ selected.fields }}</p>
          <p v-if="selected.example">将下面内容填入“参数”框，替换示例 ID 后执行：</p>
          <div v-if="selected.example" class="guide-code-head">
            <span>JSON 示例</span>
            <el-button size="small" @click="copyExample">复制示例</el-button>
          </div>
          <pre v-if="selected.example" class="guide-code">{{ example }}</pre>
          <el-alert v-else title="查询类功能只需填写上游任务 ID，账单查询直接执行。" type="info" :closable="false" />
          <p class="guide-note">先提交生成任务，拿到返回结果中的 id；查询成功后，歌曲 ID 通常位于 choices 数组的结果中。两种 ID 不要混用。</p>
          <a :href="docsUrl" target="_blank" rel="noopener noreferrer">查看官方接口文档 ↗</a>
        </el-card>

        <el-card v-else-if="topic === 'cover_advanced'" shadow="never">
          <template #header><strong>高级模式参考重唱</strong></template>
          <ol>
            <li>选择归属商户，上传 15～30 秒清唱创建演唱音色，或选择已有演唱音色。</li>
            <li>选择作品库中的原曲，或者上传原曲音频。系统把原曲作为音乐参考，参考片段约 30 秒。</li>
            <li>填写歌名和完整歌词，保留歌词原有分行，点击“开始参考重唱”。</li>
            <li>在“本次翻唱”查看任务进度，完成后试听生成的歌曲。</li>
          </ol>
          <p class="guide-note">高级模式重新生成歌曲，编曲、旋律和时长可能变化，原伴奏不会原样保留。需要只替换原曲人声时请选择普通模式。</p>
          <a href="https://platform.mureka.cn/docs/api/operations/post-v1-song-generate.html" target="_blank" rel="noopener noreferrer">查看生成歌曲接口说明 ↗</a>
        </el-card>

        <el-card v-else-if="topic === 'creation'" shadow="never">
          <template #header><strong>高级模式创作参数</strong></template>
          <p>先选创作模式，再按下面的含义填写表单。这里是普通输入框，不需要写 JSON。</p>
          <ul>
            <li><strong>灵感模式：</strong>填写音乐描述；可选模型、演唱音色、音乐参考或旋律参考。</li>
            <li><strong>自定义歌词：</strong>歌词框保留原有换行；风格标签写音乐风格，可选演唱音色。</li>
            <li><strong>纯音乐：</strong>填写音乐描述，或上传“纯音乐参考”取得文件 ID。</li>
            <li><strong>续写与混音：</strong>原歌曲 ID 和上传音频 ID 二选一。续写填写起点（界面单位为秒）和方向；混音填写新歌词与新风格。</li>
          </ul>
          <p>演唱音色来自音色上传功能；参考 ID 来自“上传参考素材”。不同用途的文件 ID 要放在对应字段。高级工具中的 JSON 时间参数使用毫秒，和这里的续写起点单位不同。</p>
          <router-link :to="{ path: '/guide', query: { tool: 'upload' } }">查看素材上传教程 →</router-link>
        </el-card>

        <el-card v-else-if="topic === 'metadata'" shadow="never">
          <template #header><strong>普通模式高级参数</strong></template>
          <p>“排除风格”选择不希望出现的曲风。“metadata”填写一个 JSON 对象，可留空；不要把整个请求或歌词放进这里。</p>
          <div class="guide-code-head">metadata 示例</div>
          <pre class="guide-code">{{ JSON.stringify({ vocal_gender: 'f', control_sliders: { style_weight: 0.87 } }, null, 2) }}</pre>
          <p><code>vocal_gender</code> 可填 <code>f</code> 或 <code>m</code>；<code>control_sliders.style_weight</code> 是数字。填写该输入框时使用合法 JSON：键和字符串用双引号、字段之间用逗号、末尾不加逗号。</p>
          <p class="guide-note">界面上的“演唱音色”属于高级模式。普通模式的 metadata 仅控制已开放的参数。</p>
        </el-card>

        <el-card v-else-if="topic === 'upload'" shadow="never">
          <template #header><strong>上传参考素材</strong></template>
          <ol>
            <li>在高级模式创作表单展开“上传参考素材”，先选择用途。</li>
            <li>选择本地文件，或填写可访问的文件 URL，然后点击上传。</li>
            <li>复制返回的文件 ID，填入相应用途的输入框。不同用途的 ID 不能随意替换。</li>
          </ol>
          <p>音乐参考对应 <code>reference_id</code>；旋律参考对应 <code>melody_id</code>；已有音频用于续写等功能时对应 <code>upload_audio_id</code>。单个本地文件最大 10 MB；较大的训练素材使用分块上传。</p>
          <router-link :to="{ path: '/guide', query: { tool: 'upload_part' } }">查看分块上传步骤 →</router-link>
        </el-card>

        <el-card v-else-if="topic === 'upload_part'" shadow="never">
          <template #header><strong>大文件分块上传</strong></template>
          <ol>
            <li>在“高级工具”中选择“创建大文件上传”，填入 <code>upload_name</code> 和 <code>purpose</code>，执行后记下 <code>upload_id</code>。</li>
            <li>在下方“大文件分块上传”处填写同一个 <code>upload_id</code>，按顺序选择文件块并逐块追加，每块不超过 10 MB。</li>
            <li>选择“完成大文件上传”，填入 <code>upload_id</code> 后执行。随后可将该 ID 用于专属模型训练。</li>
          </ol>
          <p class="guide-note">分块前请在本地将文件切分；此处每次只上传选中的一个文件块。</p>
          <router-link :to="{ path: '/guide', query: { tool: 'uploads_create' } }">查看创建上传参数 →</router-link>
        </el-card>

        <el-card v-else shadow="never">
          <template #header><strong>JSON 语法与 ID</strong></template>
          <p>高级工具的“参数”框填写一个完整 JSON 对象。切换工具后，输入框会自动给出基础示例；请按教程替换占位的 ID。</p>
          <pre class="guide-code">{{ JSON.stringify({ model: 'auto', lyrics: '[Verse]\n第一行\n第二行', prompt: 'warm pop', n: 1 }, null, 2) }}</pre>
          <ul>
            <li>键和字符串使用双引号；字段间用逗号；最后一个字段后不加逗号。JSON 中不能写注释。</li>
            <li>数字和布尔值不用引号，例如 <code>30000</code>、<code>true</code>；歌词换行在 JSON 字符串中写作 <code>\n</code>。</li>
            <li><code>task_id</code> 是提交生成任务返回的上游任务 ID，填在查询工具的独立输入框；<code>song_id</code> 是生成结果里某首歌曲的 ID。</li>
            <li><code>upload_audio_id</code>、<code>reference_id</code> 等是按对应“用途”上传后返回的文件 ID。</li>
            <li><code>extend_at</code>、<code>edit_start</code>、<code>edit_end</code> 使用毫秒，30 秒写作 <code>30000</code>。</li>
          </ul>
          <p>高级模式创作表单使用可视化字段填写常用参数；“高级工具”适合分析、编辑、视频和语音等扩展功能。</p>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.guide-nav { margin-bottom: 16px; }
.guide-nav-title { font-weight: 700; margin: 8px 0; }
.guide-nav a { display: block; padding: 7px 10px; color: var(--el-text-color-regular); text-decoration: none; border-radius: 6px; }
.guide-nav a:hover, .guide-nav a.active { color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
.guide-code-head { display: flex; align-items: center; justify-content: space-between; margin-top: 16px; font-weight: 600; }
.guide-code { margin: 8px 0 18px; padding: 16px; border-radius: 8px; background: var(--el-fill-color-light); white-space: pre-wrap; overflow-x: auto; line-height: 1.6; }
.guide-note { color: var(--el-text-color-secondary); }
.guide-page li { margin: 8px 0; line-height: 1.6; }
.guide-page p { line-height: 1.7; }
</style>
