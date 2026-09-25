<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { fetchMerchantOptions, fetchSystem, fetchTaskDetail, fetchTasks, refundTask, retryTask } from '@/api'
import { TASK_KINDS, TASK_STATUS, formatTime, playableUrl, playableVideoUrl, rangeToParams, thousands } from '@/utils/format'

const route = useRoute()

const loading = ref(false)
const list = ref([])
const total = ref(0)
const merchants = ref([])
const dateRange = ref([])
const query = reactive({
  merchant_id: '',
  kind: '',
  status: '',
  keyword: '',
  page: 1,
  size: 20
})

const kinds = TASK_KINDS
const statuses = Object.entries(TASK_STATUS).map(([value, meta]) => ({ value, label: meta.label }))

async function load() {
  loading.value = true
  try {
    const { start, end } = rangeToParams(dateRange.value)
    const data = await fetchTasks({
      merchant_id: query.merchant_id || undefined,
      kind: query.kind || undefined,
      status: query.status || undefined,
      keyword: query.keyword || undefined,
      start: start || undefined,
      end: end || undefined,
      page: query.page,
      size: query.size
    })
    list.value = data.list || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

function reset() {
  query.merchant_id = ''
  query.kind = ''
  query.status = ''
  query.keyword = ''
  dateRange.value = []
  search()
}

/* ---------------------------------- 详情 ---------------------------------- */

const detailVisible = ref(false)
const detailLoading = ref(false)
const detail = ref(null)
const requestPayload = ref('')

async function openDetail(row) {
  detailVisible.value = true
  detailLoading.value = true
  detail.value = null
  try {
    const data = await fetchTaskDetail(row.id)
    detail.value = data.task
    requestPayload.value = prettify(data.request_payload)
  } finally {
    detailLoading.value = false
  }
}

function prettify(raw) {
  if (!raw) return '—'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

const prettyExtend = () => prettify(detail.value?.extend)

/* ---------------------------------- 免费重试 ---------------------------------- */

// 生成失败不退积分，可用原参数免费重试若干次
const maxRetries = ref(3)

function canRetry(row) {
  return row.status === 'failed' && (row.retry_count || 0) < maxRetries.value
}

async function handleRetry(row) {
  await ElMessageBox.confirm(
    `将用原参数重新提交任务 #${row.id}，不额外扣积分（第 ${(row.retry_count || 0) + 1}/${maxRetries.value} 次重试）。`,
    '免费重试',
    { type: 'info', confirmButtonText: '重试' }
  )
  await retryTask(row.id)
  ElMessage.success('已重新提交，稍后刷新查看结果')
  load()
  if (detailVisible.value) detailVisible.value = false
}

/* ---------------------------------- 人工退款 ---------------------------------- */

// 失败任务默认不退积分，人工退款仅供客服特殊情况下使用
async function handleRefund(row) {
  await ElMessageBox.confirm(
    `失败任务默认不退积分、可免费重试。确定要向「${row.merchant_name}」人工退还 ${row.points_cost} 积分吗？`,
    '人工退款',
    { type: 'warning' }
  )
  const data = await refundTask({ id: row.id })
  ElMessage.success(`已退还 ${thousands(data.points)} 积分`)
  load()
  if (detailVisible.value) detailVisible.value = false
}

function canRefund(row) {
  return row.status === 'failed' && !row.points_refunded && row.points_cost > 0
}

onMounted(async () => {
  fetchSystem()
    .then((s) => (maxRetries.value = s.task_max_retries || 3))
    .catch(() => {})
  if (route.query.merchant_id) query.merchant_id = Number(route.query.merchant_id)
  merchants.value = (await fetchMerchantOptions()) || []
  load()
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>任务管理</h2>
        <p class="desc">共 {{ total }} 条任务记录</p>
      </div>
      <el-button :icon="'Refresh'" @click="load">刷新</el-button>
    </div>

    <el-card shadow="never" body-style="padding:16px">
      <el-form :inline="true" class="filter-bar" @submit.prevent>
        <el-form-item label="商户">
          <el-select v-model="query.merchant_id" placeholder="全部" clearable filterable style="width: 190px">
            <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（${m.id}）`" :value="m.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="query.kind" placeholder="全部" clearable style="width: 150px">
            <el-option v-for="k in kinds" :key="k.value" :label="k.label" :value="k.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="query.status" placeholder="全部" clearable style="width: 120px">
            <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="关键词">
          <el-input v-model="query.keyword" placeholder="任务 ID 或 custom_id" clearable style="width: 200px" @keyup.enter="search" />
        </el-form-item>
        <el-form-item label="时间">
          <el-date-picker
            v-model="dateRange"
            type="daterange"
            value-format="YYYY-MM-DD"
            start-placeholder="开始"
            end-placeholder="结束"
            style="width: 240px"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="'Search'" @click="search">查询</el-button>
          <el-button @click="reset">重置</el-button>
        </el-form-item>
      </el-form>

      <el-table v-loading="loading" :data="list" stripe border style="width: 100%">
        <el-table-column prop="id" label="任务 ID" width="90" />
        <el-table-column prop="merchant_name" label="商户" min-width="120" show-overflow-tooltip />
        <el-table-column prop="kind_label" label="类型" width="130" />
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="TASK_STATUS[row.status]?.type || 'info'" size="small">
              {{ TASK_STATUS[row.status]?.label || row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="custom_id" min-width="180">
          <template #default="{ row }">
            <span class="mono">{{ row.custom_id || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="消耗积分" width="110" align="right">
          <template #default="{ row }">
            <span>{{ row.points_cost }}</span>
            <el-tag v-if="row.points_refunded" type="primary" size="small" style="margin-left: 4px">已退</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">
            <span class="mono">{{ formatTime(row.created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="完成时间" width="160">
          <template #default="{ row }">
            <span class="mono">{{ formatTime(row.finished_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="190" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
            <el-button v-if="canRetry(row)" link type="success" @click="handleRetry(row)">免费重试</el-button>
            <el-button v-if="canRefund(row)" link type="warning" @click="handleRefund(row)">退款</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @current-change="load"
          @size-change="search"
        />
      </div>
    </el-card>

    <el-drawer v-model="detailVisible" title="任务详情" size="620px">
      <div v-loading="detailLoading">
        <template v-if="detail">
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item label="任务 ID">{{ detail.id }}</el-descriptions-item>
            <el-descriptions-item label="商户">{{ detail.merchant_name }}</el-descriptions-item>
            <el-descriptions-item label="类型">{{ detail.kind_label }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="TASK_STATUS[detail.status]?.type || 'info'" size="small">
                {{ TASK_STATUS[detail.status]?.label || detail.status }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="消耗积分">{{ detail.points_cost }}</el-descriptions-item>
            <el-descriptions-item label="是否已退">{{ detail.points_refunded ? '是' : '否' }}</el-descriptions-item>
            <el-descriptions-item label="custom_id" :span="2">
              <span class="mono">{{ detail.custom_id || '—' }}</span>
            </el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ formatTime(detail.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="完成时间">{{ formatTime(detail.finished_at) }}</el-descriptions-item>
            <el-descriptions-item v-if="detail.error_message" label="失败原因" :span="2">
              <span style="color: #f56c6c">{{ detail.error_message }}</span>
            </el-descriptions-item>
          </el-descriptions>

          <template v-if="detail.fileInfo">
            <h4>产出文件</h4>
            <div v-if="playableUrl(detail)" style="margin-bottom: 8px">
              <audio :src="playableUrl(detail)" controls preload="none" style="width: 100%"></audio>
              <div class="text-muted" style="font-size: 12px; margin-top: 4px">
                链接 1 小时内有效，过期后重新查询任务可获取新地址
              </div>
            </div>
            <video
              v-if="detail.fileInfo.mp4Url"
              :src="playableVideoUrl(detail)"
              controls
              preload="none"
              style="width: 100%; border-radius: 6px"
            ></video>
            <el-descriptions :column="1" border size="small" style="margin-top: 8px">
              <el-descriptions-item v-if="detail.fileInfo.coverUrl" label="封面">
                <el-link :href="detail.fileInfo.coverUrl" target="_blank" type="primary" class="mono">
                  {{ detail.fileInfo.coverUrl }}
                </el-link>
              </el-descriptions-item>
              <el-descriptions-item v-if="detail.fileInfo.duration" label="时长">
                {{ detail.fileInfo.duration }} 秒
              </el-descriptions-item>
            </el-descriptions>
          </template>

          <h4>请求参数</h4>
          <div class="code-block">{{ requestPayload }}</div>

          <h4>上游返回（extend）</h4>
          <div class="code-block">{{ prettyExtend() }}</div>

          <div v-if="canRetry(detail) || canRefund(detail)" style="margin-top: 16px">
            <el-button v-if="canRetry(detail)" type="success" @click="handleRetry(detail)">
              免费重试（已重试 {{ detail.retry_count || 0 }}/{{ maxRetries }} 次）
            </el-button>
            <el-button v-if="canRefund(detail)" type="warning" plain @click="handleRefund(detail)">人工退还积分</el-button>
          </div>
        </template>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped>
h4 {
  margin: 20px 0 8px;
  font-size: 14px;
}
</style>
