<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  adjustPoints,
  createMerchant,
  fetchMerchants,
  renameMerchant,
  setMerchantStatus
} from '@/api'
import { formatTime, thousands, toAmount } from '@/utils/format'

const router = useRouter()

const loading = ref(false)
const list = ref([])
const total = ref(0)
const query = reactive({ keyword: '', status: '', page: 1, size: 20 })

async function load() {
  loading.value = true
  try {
    const data = await fetchMerchants({
      keyword: query.keyword,
      status: query.status === '' ? undefined : query.status,
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
  query.keyword = ''
  query.status = ''
  search()
}

/* ---------------------------------- 新建商户 ---------------------------------- */

const createVisible = ref(false)
const creating = ref(false)
const createForm = reactive({ name: '', points: 60, create_key: true })
const createdKey = ref('')

function openCreate() {
  createForm.name = ''
  createForm.points = 60
  createForm.create_key = true
  createdKey.value = ''
  createVisible.value = true
}

async function submitCreate() {
  if (!createForm.name.trim()) {
    ElMessage.warning('请填写商户名称')
    return
  }
  creating.value = true
  try {
    const data = await createMerchant({ ...createForm })
    ElMessage.success('商户创建成功')
    if (data.access_key) {
      createdKey.value = data.access_key
    } else {
      createVisible.value = false
    }
    load()
  } finally {
    creating.value = false
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(createdKey.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动选中复制')
  }
}

/* ---------------------------------- 行操作 ---------------------------------- */

async function toggleStatus(row) {
  const next = row.status === 1 ? 0 : 1
  const action = next === 1 ? '启用' : '禁用'
  await ElMessageBox.confirm(`确定${action}商户「${row.name}」吗？禁用后其密钥将无法调用接口。`, '提示', {
    type: 'warning'
  })
  await setMerchantStatus({ id: row.id, status: next })
  ElMessage.success(`${action}成功`)
  load()
}

async function rename(row) {
  const { value } = await ElMessageBox.prompt('请输入新的商户名称', '重命名', {
    inputValue: row.name,
    inputValidator: (v) => (v && v.trim() ? true : '名称不能为空')
  })
  await renameMerchant({ id: row.id, name: value.trim() })
  ElMessage.success('已更新')
  load()
}

/* ---------------------------------- 积分调整 ---------------------------------- */

const pointVisible = ref(false)
const pointSaving = ref(false)
const pointForm = reactive({ merchant_id: 0, merchant_name: '', points: 100, type: 2, remark: '' })

function openAdjust(row) {
  pointForm.merchant_id = row.id
  pointForm.merchant_name = row.name
  pointForm.points = 100
  pointForm.type = 2
  pointForm.remark = ''
  pointVisible.value = true
}

async function submitAdjust() {
  if (!pointForm.points) {
    ElMessage.warning('变动积分不能为 0')
    return
  }
  pointSaving.value = true
  try {
    const data = await adjustPoints({
      merchant_id: pointForm.merchant_id,
      points: pointForm.points,
      type: pointForm.type,
      remark: pointForm.remark
    })
    ElMessage.success(`操作成功，当前余额 ${thousands(data.balance)}`)
    pointVisible.value = false
    load()
  } finally {
    pointSaving.value = false
  }
}

function viewKeys(row) {
  router.push({ name: 'keys', query: { merchant_id: row.id } })
}

function viewTasks(row) {
  router.push({ name: 'tasks', query: { merchant_id: row.id } })
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>商户管理</h2>
        <p class="desc">共 {{ total }} 个商户</p>
      </div>
      <el-button type="primary" :icon="'Plus'" @click="openCreate">新建商户</el-button>
    </div>

    <el-card shadow="never" body-style="padding:16px">
      <el-form :inline="true" class="filter-bar" @submit.prevent>
        <el-form-item label="关键词">
          <el-input v-model="query.keyword" placeholder="商户名称或 ID" clearable style="width: 200px" @keyup.enter="search" />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="query.status" placeholder="全部" clearable style="width: 120px">
            <el-option label="正常" :value="1" />
            <el-option label="已禁用" :value="0" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="'Search'" @click="search">查询</el-button>
          <el-button @click="reset">重置</el-button>
        </el-form-item>
      </el-form>

      <el-table v-loading="loading" :data="list" stripe border style="width: 100%">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="name" label="商户名称" min-width="140" show-overflow-tooltip />
        <el-table-column label="积分余额" width="130" align="right">
          <template #default="{ row }">
            <div style="font-weight: 600">{{ thousands(row.points) }}</div>
            <div class="text-muted mono">{{ toAmount(row.points) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="累计消耗" width="120" align="right">
          <template #default="{ row }">{{ thousands(row.consumed_total) }}</template>
        </el-table-column>
        <el-table-column label="密钥" width="80" align="center">
          <template #default="{ row }">
            <el-link type="primary" @click="viewKeys(row)">{{ row.key_count }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="任务" width="90" align="center">
          <template #default="{ row }">
            <el-link type="primary" @click="viewTasks(row)">{{ thousands(row.task_count) }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
              {{ row.status === 1 ? '正常' : '已禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最近活跃" width="160">
          <template #default="{ row }">
            <span class="mono">{{ formatTime(row.last_active_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">
            <span class="mono">{{ formatTime(row.created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="230" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openAdjust(row)">调整积分</el-button>
            <el-button link type="primary" @click="rename(row)">重命名</el-button>
            <el-button link :type="row.status === 1 ? 'danger' : 'success'" @click="toggleStatus(row)">
              {{ row.status === 1 ? '禁用' : '启用' }}
            </el-button>
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

    <!-- 新建商户 -->
    <el-dialog v-model="createVisible" title="新建商户" width="480px">
      <template v-if="!createdKey">
        <el-form :model="createForm" label-width="96px">
          <el-form-item label="商户名称" required>
            <el-input v-model="createForm.name" placeholder="例如：某某科技" />
          </el-form-item>
          <el-form-item label="赠送积分">
            <el-input-number v-model="createForm.points" :min="0" :step="10" />
            <span class="text-muted" style="margin-left: 8px">{{ toAmount(createForm.points) }}</span>
          </el-form-item>
          <el-form-item label="同时建密钥">
            <el-switch v-model="createForm.create_key" />
            <span class="text-muted" style="margin-left: 8px">明文只展示一次</span>
          </el-form-item>
        </el-form>
      </template>

      <template v-else>
        <el-alert type="success" :closable="false" show-icon title="商户已创建，请立即保存 access_key">
          数据库只保存哈希值，关闭后无法再次查看。
        </el-alert>
        <div class="code-block" style="margin-top: 12px">{{ createdKey }}</div>
      </template>

      <template #footer>
        <template v-if="!createdKey">
          <el-button @click="createVisible = false">取消</el-button>
          <el-button type="primary" :loading="creating" @click="submitCreate">创建</el-button>
        </template>
        <template v-else>
          <el-button @click="copyKey">复制密钥</el-button>
          <el-button type="primary" @click="createVisible = false">我已保存</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- 调整积分 -->
    <el-dialog v-model="pointVisible" title="调整积分" width="440px">
      <el-form :model="pointForm" label-width="96px">
        <el-form-item label="商户">
          <span>{{ pointForm.merchant_name }}（ID {{ pointForm.merchant_id }}）</span>
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="pointForm.type">
            <el-radio-button :value="2">充值</el-radio-button>
            <el-radio-button :value="3">手动调整</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="变动积分">
          <el-input-number v-model="pointForm.points" :step="10" />
          <div class="text-muted" style="font-size: 12px; margin-top: 4px">
            {{ pointForm.type === 3 ? '手动调整可填负数扣减' : '充值必须为正数' }}，{{ toAmount(pointForm.points) }}
          </div>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="pointForm.remark" placeholder="选填，会记入流水" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pointVisible = false">取消</el-button>
        <el-button type="primary" :loading="pointSaving" @click="submitAdjust">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>
