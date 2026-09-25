<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchCapabilities, fetchMerchants, fetchTaskDetail, retryTask, studioGenerate } from '@/api'
import { TASK_STATUS, playableVideoUrl, thousands } from '@/utils/format'

const GROUPS = [
  { key: 'image', label: '图片生成' },
  { key: 'video', label: '视频生成' }
]

const group = ref('image')
const capabilities = ref([])
const merchants = ref([])
const submitting = ref(false)

const currentKind = ref('')
const form = reactive({ merchant_id: '', payload: {} })

const groupCaps = computed(() => capabilities.value.filter((c) => c.group === group.value))
const current = computed(() => capabilities.value.find((c) => c.kind === currentKind.value))

// 同一家服务商的模型归在一起展示
const vendors = computed(() => {
  const map = new Map()
  for (const cap of groupCaps.value) {
    if (!map.has(cap.vendor)) map.set(cap.vendor, [])
    map.get(cap.vendor).push(cap)
  }
  return [...map.entries()].map(([vendor, items]) => ({ vendor, items }))
})

const currentMerchant = computed(() => merchants.value.find((m) => m.id === form.merchant_id))

const balanceEnough = computed(() => {
  if (!currentMerchant.value || !current.value) return true
  return currentMerchant.value.points >= current.value.price
})

/* ---------------------------------- 加载 ---------------------------------- */

async function loadCapabilities() {
  const data = await fetchCapabilities()
  capabilities.value = data.list || []
  pickFirst()
}

async function loadMerchants() {
  const data = await fetchMerchants({ page: 1, size: 200, status: 1 })
  merchants.value = data.list || []
  if (!form.merchant_id && merchants.value.length) form.merchant_id = merchants.value[0].id
}

function pickFirst() {
  const first = groupCaps.value[0]
  if (first) selectCap(first.kind)
}

/** 切换能力时按参数定义重建表单默认值 */
function selectCap(kind) {
  currentKind.value = kind
  const cap = capabilities.value.find((c) => c.kind === kind)
  const next = {}
  for (const p of cap?.params || []) {
    if (p.type === 'array') next[p.name] = []
    else if (p.type === 'boolean') next[p.name] = p.default ?? false
    else next[p.name] = p.default ?? ''
  }
  form.payload = next
}

watch(group, pickFirst)

/* ---------------------------------- 提交 ---------------------------------- */

async function submit() {
  if (!form.merchant_id) {
    ElMessage.warning('请选择归属商户')
    return
  }
  if (!current.value) return

  for (const p of current.value.params) {
    const v = form.payload[p.name]
    const blank = v === '' || v === null || v === undefined || (Array.isArray(v) && v.length === 0)
    if (p.required && blank) {
      ElMessage.warning(`请填写${p.label || p.name}`)
      return
    }
  }

  submitting.value = true
  try {
    const data = await studioGenerate({
      merchant_id: form.merchant_id,
      kind: current.value.kind,
      payload: form.payload
    })
    ElMessage.success(`已提交，扣除 ${data.cost} 积分，余额 ${thousands(data.balance)}`)
    startTracking(data.task_ids, current.value)
    loadMerchants()
  } finally {
    submitting.value = false
  }
}

/* ---------------------------------- 轮询 ---------------------------------- */

const tracking = ref([])
let timer = null

function startTracking(ids, cap) {
  tracking.value = ids.map((id) => ({
    id,
    label: cap.label,
    group: cap.group,
    status: 'pending',
    elapsed: 0,
    task: null
  }))
  if (!timer) {
    timer = setInterval(tick, 4000)
    tick()
  }
}

// 生成失败不退积分，可用原参数免费重试，任务编号不变
async function retryItem(item) {
  item.retrying = true
  try {
    await retryTask(item.id)
    Object.assign(item, { status: 'pending', elapsed: 0, task: null })
    ElMessage.success('已重新提交')
    if (!timer) {
      timer = setInterval(tick, 4000)
      tick()
    }
  } finally {
    item.retrying = false
  }
}

async function tick() {
  const pending = tracking.value.filter((t) => t.status !== 'completed' && t.status !== 'failed')
  if (!pending.length) {
    clearInterval(timer)
    timer = null
    return
  }

  await Promise.all(
    pending.map(async (item) => {
      try {
        const data = await fetchTaskDetail(item.id)
        item.task = data.task
        item.status = data.task.status
        item.elapsed += 4
      } catch {
        // 单次失败不中断，下个周期重试
      }
    })
  )
}

const imagesOf = (item) => item.task?.fileInfo?.images || []
const videoOf = (item) => playableVideoUrl(item.task)

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选中复制')
  }
}

/* ---------------------------------- 参考图 ---------------------------------- */

const refInput = reactive({})

function addRef(name) {
  const url = (refInput[name] || '').trim()
  if (!url) return
  if (!/^https?:\/\//.test(url)) {
    ElMessage.warning('请填写 http/https 开头的图片地址')
    return
  }
  form.payload[name] = [...(form.payload[name] || []), url]
  refInput[name] = ''
}

function removeRef(name, index) {
  form.payload[name] = form.payload[name].filter((_, i) => i !== index)
}

onMounted(() => {
  loadCapabilities()
  loadMerchants()
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>图像视频</h2>
        <p class="desc">与音乐共用同一个上游账户与密钥，积分从所选商户扣除</p>
      </div>
      <el-radio-group v-model="group">
        <el-radio-button v-for="g in GROUPS" :key="g.key" :value="g.key">{{ g.label }}</el-radio-button>
      </el-radio-group>
    </div>

    <el-row :gutter="12">
      <!-- 模型选择 -->
      <el-col :xs="24" :lg="6" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:12px">
          <template #header><span>选择模型</span></template>
          <div v-for="v in vendors" :key="v.vendor" class="vendor">
            <div class="vendor-name">{{ v.vendor }}</div>
            <div
              v-for="cap in v.items"
              :key="cap.kind"
              class="cap-item"
              :class="{ active: cap.kind === currentKind }"
              @click="selectCap(cap.kind)"
            >
              <div class="cap-label">{{ cap.label }}</div>
              <div class="cap-desc">{{ cap.desc }}</div>
              <div class="cap-price">{{ cap.price }} 积分/次</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <!-- 参数表单 -->
      <el-col :xs="24" :lg="10" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header>
            <span>{{ current?.label || '创作参数' }}</span>
          </template>

          <el-form :model="form" label-width="92px">
            <el-form-item label="归属商户" required>
              <el-select v-model="form.merchant_id" filterable placeholder="请选择商户" style="width: 100%">
                <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（余额 ${m.points}）`" :value="m.id" />
              </el-select>
              <div v-if="currentMerchant && current" class="text-muted" style="font-size: 12px; margin-top: 4px">
                本次消耗 {{ current.price }} 积分，当前余额 {{ thousands(currentMerchant.points) }}
                <span v-if="!balanceEnough" style="color: #f56c6c">（余额不足）</span>
              </div>
            </el-form-item>

            <template v-for="p in current?.params || []" :key="p.name">
              <el-form-item :label="p.label || p.name" :required="p.required">
                <!-- 参考图数组 -->
                <template v-if="p.type === 'array'">
                  <div style="width: 100%">
                    <div v-for="(url, i) in form.payload[p.name] || []" :key="i" class="ref-row">
                      <span class="mono ref-url" :title="url">{{ url }}</span>
                      <el-button link type="danger" size="small" @click="removeRef(p.name, i)">移除</el-button>
                    </div>
                    <div style="display: flex; gap: 8px">
                      <el-input
                        v-model="refInput[p.name]"
                        placeholder="粘贴图片 URL 后回车添加"
                        @keyup.enter="addRef(p.name)"
                      />
                      <el-button @click="addRef(p.name)">添加</el-button>
                    </div>
                  </div>
                </template>

                <el-select v-else-if="p.options?.length" v-model="form.payload[p.name]" clearable style="width: 100%">
                  <el-option v-for="opt in p.options" :key="opt" :label="opt" :value="opt" />
                </el-select>

                <el-switch v-else-if="p.type === 'boolean'" v-model="form.payload[p.name]" />

                <el-input-number v-else-if="p.type === 'number'" v-model="form.payload[p.name]" :step="1" />

                <el-input
                  v-else
                  v-model="form.payload[p.name]"
                  :type="p.multiline ? 'textarea' : 'text'"
                  :rows="p.multiline ? 4 : undefined"
                />

                <div v-if="p.description" class="text-muted" style="font-size: 12px; margin-top: 4px">
                  {{ p.description }}
                </div>
              </el-form-item>
            </template>

            <el-form-item>
              <el-button
                type="primary"
                size="large"
                :loading="submitting"
                :disabled="!balanceEnough || !current"
                @click="submit"
              >
                提交生成（扣 {{ current?.price || 0 }} 积分）
              </el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <!-- 生成结果 -->
      <el-col :xs="24" :lg="8" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>生成结果</span></template>

          <el-empty v-if="!tracking.length" description="提交后在这里查看进度与结果" :image-size="80" />

          <div v-for="item in tracking" :key="item.id" class="result-card">
            <div class="result-head">
              <span class="label">{{ item.label }}</span>
              <span class="mono text-muted">#{{ item.id }}</span>
              <el-tag :type="TASK_STATUS[item.status]?.type || 'info'" size="small" style="margin-left: auto">
                {{ TASK_STATUS[item.status]?.label || item.status }}
              </el-tag>
            </div>

            <template v-if="item.status === 'pending' || item.status === 'processing'">
              <el-progress :percentage="Math.min(95, item.elapsed * 3)" :show-text="false" :stroke-width="4" />
              <div class="text-muted" style="font-size: 12px; margin-top: 4px">
                已等待 {{ item.elapsed }} 秒，每 4 秒自动查询
              </div>
            </template>

            <template v-else-if="item.status === 'completed'">
              <div v-if="imagesOf(item).length" class="img-grid">
                <el-image
                  v-for="(url, i) in imagesOf(item)"
                  :key="i"
                  :src="url"
                  fit="cover"
                  :preview-src-list="imagesOf(item)"
                  :initial-index="i"
                  preview-teleported
                  class="result-img"
                />
              </div>

              <video
                v-else-if="videoOf(item)"
                :src="videoOf(item)"
                controls
                preload="metadata"
                style="width: 100%; border-radius: 6px; background: #000; margin-top: 6px"
              ></video>

              <p v-else class="text-muted" style="font-size: 12px">任务已完成，但未返回可展示的素材</p>

              <el-button
                v-if="imagesOf(item).length || videoOf(item)"
                link
                type="primary"
                size="small"
                style="margin-top: 6px"
                @click="copyText(imagesOf(item)[0] || videoOf(item))"
              >
                复制地址
              </el-button>
            </template>

            <div v-else-if="item.status === 'failed'" style="color: #f56c6c; font-size: 13px">
              {{ item.task?.error_message || '生成失败' }}，积分不退还
              <el-button link type="primary" size="small" :loading="item.retrying" @click="retryItem(item)">
                免费重试
              </el-button>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped lang="scss">
.vendor {
  margin-bottom: 14px;

  .vendor-name {
    font-size: 12px;
    color: #909399;
    margin-bottom: 6px;
    padding-left: 2px;
  }
}

.cap-item {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 8px 10px;
  margin-bottom: 6px;
  cursor: pointer;
  transition: all 0.15s;

  &:hover {
    border-color: #c6e2ff;
  }

  &.active {
    border-color: #409eff;
    background: #ecf5ff;
  }

  .cap-label {
    font-size: 13px;
    font-weight: 600;
  }

  .cap-desc {
    font-size: 11px;
    color: #909399;
    margin-top: 2px;
    line-height: 1.4;
  }

  .cap-price {
    font-size: 11px;
    color: #e6a23c;
    margin-top: 4px;
  }
}

.ref-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;

  .ref-url {
    flex: 1;
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: #606266;
  }
}

.result-card {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 10px;

  .result-head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
  }

  .label {
    font-size: 13px;
    font-weight: 600;
  }
}

.img-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 6px;
}

.result-img {
  width: 100%;
  aspect-ratio: 1 / 1;
  border-radius: 4px;
}
</style>
