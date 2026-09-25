<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createKey, deleteKey, fetchKeys, fetchMerchantOptions, setKeyStatus } from '@/api'
import { formatTime } from '@/utils/format'

const route = useRoute()

const loading = ref(false)
const list = ref([])
const total = ref(0)
const merchants = ref([])
const query = reactive({ merchant_id: '', page: 1, size: 20 })

async function load() {
  loading.value = true
  try {
    const data = await fetchKeys({
      merchant_id: query.merchant_id || undefined,
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

/* ---------------------------------- 新建密钥 ---------------------------------- */

const createVisible = ref(false)
const creating = ref(false)
const createForm = reactive({ merchant_id: '', name: 'default' })
const createdKey = ref('')

function openCreate() {
  createForm.merchant_id = query.merchant_id || ''
  createForm.name = 'default'
  createdKey.value = ''
  createVisible.value = true
}

async function submitCreate() {
  if (!createForm.merchant_id) {
    ElMessage.warning('请选择商户')
    return
  }
  creating.value = true
  try {
    const data = await createKey({ merchant_id: createForm.merchant_id, name: createForm.name })
    createdKey.value = data.access_key
    ElMessage.success('密钥已生成')
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
  await ElMessageBox.confirm(`确定${action}该密钥吗？禁用后使用它的调用会立即返回 401。`, '提示', { type: 'warning' })
  await setKeyStatus({ id: row.id, status: next })
  ElMessage.success(`${action}成功`)
  load()
}

async function remove(row) {
  await ElMessageBox.confirm('删除后不可恢复，使用该密钥的调用会立即失效。确定删除吗？', '删除密钥', {
    type: 'warning',
    confirmButtonText: '删除',
    confirmButtonClass: 'el-button--danger'
  })
  await deleteKey({ id: row.id })
  ElMessage.success('已删除')
  load()
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
        <h2>密钥管理</h2>
        <p class="desc">共 {{ total }} 条密钥，数据库只保存哈希，明文仅在生成时展示一次</p>
      </div>
      <el-button type="primary" :icon="'Plus'" @click="openCreate">生成密钥</el-button>
    </div>

    <el-card shadow="never" body-style="padding:16px">
      <el-form :inline="true" class="filter-bar" @submit.prevent>
        <el-form-item label="商户">
          <el-select v-model="query.merchant_id" placeholder="全部商户" clearable filterable style="width: 220px">
            <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（${m.id}）`" :value="m.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="'Search'" @click="search">查询</el-button>
        </el-form-item>
      </el-form>

      <el-table v-loading="loading" :data="list" stripe border style="width: 100%">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="merchant_name" label="所属商户" min-width="140" show-overflow-tooltip />
        <el-table-column prop="name" label="名称" width="120" />
        <el-table-column label="密钥前缀" width="180">
          <template #default="{ row }">
            <span class="mono">{{ row.prefix }}••••••</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
              {{ row.status === 1 ? '启用' : '已禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最近使用" width="170">
          <template #default="{ row }">
            <span class="mono">{{ formatTime(row.last_used_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">
            <span class="mono">{{ formatTime(row.created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link :type="row.status === 1 ? 'warning' : 'success'" @click="toggleStatus(row)">
              {{ row.status === 1 ? '禁用' : '启用' }}
            </el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
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

    <el-dialog v-model="createVisible" title="生成密钥" width="480px">
      <template v-if="!createdKey">
        <el-form :model="createForm" label-width="80px">
          <el-form-item label="商户" required>
            <el-select v-model="createForm.merchant_id" placeholder="请选择商户" filterable style="width: 100%">
              <el-option v-for="m in merchants" :key="m.id" :label="`${m.name}（${m.id}）`" :value="m.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="名称">
            <el-input v-model="createForm.name" placeholder="便于区分用途，如 生产 / 测试" />
          </el-form-item>
        </el-form>
      </template>

      <template v-else>
        <el-alert type="success" :closable="false" show-icon title="密钥已生成，请立即保存">
          关闭后无法再次查看完整密钥。
        </el-alert>
        <div class="code-block" style="margin-top: 12px">{{ createdKey }}</div>
      </template>

      <template #footer>
        <template v-if="!createdKey">
          <el-button @click="createVisible = false">取消</el-button>
          <el-button type="primary" :loading="creating" @click="submitCreate">生成</el-button>
        </template>
        <template v-else>
          <el-button @click="copyKey">复制密钥</el-button>
          <el-button type="primary" @click="createVisible = false">我已保存</el-button>
        </template>
      </template>
    </el-dialog>
  </div>
</template>
