<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { adjustPoints, fetchMerchantOptions, fetchPointLogs } from '@/api'
import { ElMessage } from 'element-plus'
import { POINT_TYPES, formatTime, rangeToParams, thousands, toAmount } from '@/utils/format'

const route = useRoute()

const loading = ref(false)
const list = ref([])
const total = ref(0)
const merchants = ref([])
const dateRange = ref([])
const query = reactive({ merchant_id: '', type: '', page: 1, size: 20 })

const types = Object.entries(POINT_TYPES).map(([value, meta]) => ({ value: Number(value), label: meta.label }))

async function load() {
  loading.value = true
  try {
    const { start, end } = rangeToParams(dateRange.value)
    const data = await fetchPointLogs({
      merchant_id: query.merchant_id || undefined,
      type: query.type || undefined,
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
  query.type = ''
  dateRange.value = []
  search()
}

/** 当前页的收支小计，便于快速核对 */
const pageSummary = computed(() => {
  let income = 0
  let expense = 0
  for (const row of list.value) {
    if (row.points >= 0) income += row.points
    else expense += -row.points
  }
  return { income, expense }
})

/* ---------------------------------- 充值 / 调整 ---------------------------------- */

const formVisible = ref(false)
const saving = ref(false)
const form = reactive({ merchant_id: '', points: 100, type: 2, remark: '' })

function openForm() {
  form.merchant_id = query.merchant_id || ''
  form.points = 100
  form.type = 2
  form.remark = ''
  formVisible.value = true
}

async function submit() {
  if (!form.merchant_id) {
    ElMessage.warning('请选择商户')
    return
  }
  if (!form.points) {
    ElMessage.warning('变动积分不能为 0')
    return
  }

  saving.value = true
  try {
    const data = await adjustPoints({ ...form })
    ElMessage.success(`操作成功，当前余额 ${thousands(data.balance)}`)
    formVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  if (route.query.merchant_id) query.merchant_id = Number(route.query.merchant_id)
  merchants.value = (await fetchMerchantOptions()) || []
  load()
})
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>历史积分流水</h2>
        <p class="desc">
          共 {{ total }} 条记录，本页入账 {{ thousands(pageSummary.income) }}、出账 {{ thousands(pageSummary.expense) }}
        </p>
      </div>
      <el-button type="primary" :icon="'Plus'" @click="openForm">充值 / 调整</el-button>
    </div>

    <el-card shadow="never" body-style="padding:16px">
      <el-form :inline="true" class="filter-bar" @submit.prevent>
        <el-form-item label="商户">
          <el-select v-model="query.merchant_id" placeholder="全部" clearable filterable style="width: 190px">
            <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（${m.id}）`" :value="m.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="query.type" placeholder="全部" clearable style="width: 130px">
            <el-option v-for="t in types" :key="t.value" :label="t.label" :value="t.value" />
          </el-select>
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
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="merchant_name" label="商户" min-width="140" show-overflow-tooltip />
        <el-table-column label="类型" width="110" align="center">
          <template #default="{ row }">
            <el-tag :type="POINT_TYPES[row.type]?.type || 'info'" size="small">
              {{ POINT_TYPES[row.type]?.label || row.type }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="变动" width="130" align="right">
          <template #default="{ row }">
            <span :style="{ color: row.points >= 0 ? '#1baf7a' : '#eb6834', fontWeight: 600 }">
              {{ row.points >= 0 ? '+' : '' }}{{ thousands(row.points) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="变动后余额" width="130" align="right">
          <template #default="{ row }">
            <div>{{ thousands(row.balance) }}</div>
            <div class="text-muted mono">{{ toAmount(row.balance) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="关联任务" width="100" align="center">
          <template #default="{ row }">
            <span class="mono">{{ row.task_id || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="200" show-overflow-tooltip />
        <el-table-column label="时间" width="170">
          <template #default="{ row }">
            <span class="mono">{{ formatTime(row.created_at) }}</span>
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

    <el-dialog v-model="formVisible" title="充值 / 调整积分" width="440px">
      <el-form :model="form" label-width="96px">
        <el-form-item label="商户" required>
          <el-select v-model="form.merchant_id" placeholder="请选择商户" filterable style="width: 100%">
            <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（${m.id}）`" :value="m.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="form.type">
            <el-radio-button :value="2">充值</el-radio-button>
            <el-radio-button :value="3">手动调整</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="变动积分">
          <el-input-number v-model="form.points" :step="10" />
          <div class="text-muted" style="font-size: 12px; margin-top: 4px">
            {{ form.type === 3 ? '手动调整可填负数扣减' : '充值必须为正数' }}，{{ toAmount(form.points) }}
          </div>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" placeholder="选填，会记入流水" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>
