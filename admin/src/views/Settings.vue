<script setup>
import { onMounted, ref } from 'vue'
import { fetchModels, fetchSystem } from '@/api'
import { toAmount } from '@/utils/format'

const loading = ref(false)
const system = ref(null)
const models = ref(null)

const STATUS_TAG = {
  active: { label: '当前版本', type: 'success' },
  legacy: { label: '旧版本', type: 'info' },
  deprecated: { label: '已下线', type: 'warning' }
}

const MODEL_GROUPS = [
  { key: 'generate', title: '生成音乐（mv）' },
  { key: 'sound', title: '生成音效（mv）' },
  { key: 'remaster', title: 'Remaster（model_name）' }
]

async function load() {
  loading.value = true
  try {
    const [s, m] = await Promise.all([fetchSystem(), fetchModels()])
    system.value = s
    models.value = m
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div>
        <h2>系统设置</h2>
        <p class="desc">以下为后端当前生效的运行参数，修改需调整 backend/.env 并重启服务</p>
      </div>
      <el-button :icon="'Refresh'" @click="load">刷新</el-button>
    </div>

    <template v-if="system">
      <el-row :gutter="12">
        <el-col :xs="24" :lg="12" style="margin-bottom: 12px">
          <el-card shadow="never" body-style="padding:16px">
            <template #header><span>上游与任务</span></template>
            <el-descriptions :column="1" border size="small">
              <el-descriptions-item label="Provider">
                <el-tag :type="system.provider === 'suno' ? 'success' : 'warning'" size="small">
                  {{ system.provider === 'suno' ? '真实上游' : '本地 Mock' }}
                </el-tag>
                <span class="text-muted" style="margin-left: 8px">SUNO_PROVIDER</span>
              </el-descriptions-item>
              <el-descriptions-item label="上游地址">
                <span class="mono">{{ system.upstream_base }}</span>
              </el-descriptions-item>
              <el-descriptions-item label="上游密钥">
                <el-tag :type="system.upstream_configured ? 'success' : 'info'" size="small">
                  {{ system.upstream_configured ? '已配置' : '未配置' }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="轮询间隔">{{ system.poll_interval }}</el-descriptions-item>
              <el-descriptions-item label="轮询并发">{{ system.poll_workers }}</el-descriptions-item>
              <el-descriptions-item label="任务超时">
                {{ system.task_timeout }}
                <span class="text-muted" style="margin-left: 8px">
                  超时判失败，不退积分，可免费重试 {{ system.task_max_retries }} 次
                </span>
              </el-descriptions-item>
            </el-descriptions>
          </el-card>
        </el-col>

        <el-col :xs="24" :lg="12" style="margin-bottom: 12px">
          <el-card shadow="never" body-style="padding:16px">
            <template #header><span>限额与赠送</span></template>
            <el-descriptions :column="1" border size="small">
              <el-descriptions-item label="单商户限流">
                {{ system.rate_limit_per_minute }} 次/分钟
                <span class="text-muted" style="margin-left: 8px">超出返回 429</span>
              </el-descriptions-item>
              <el-descriptions-item label="新商户赠送">{{ system.signup_bonus }} 积分</el-descriptions-item>
              <el-descriptions-item label="版权音频附加费">
                {{ system.copyright_surcharge }} 积分
                <span class="text-muted" style="margin-left: 8px">上传时传 copyrightAudio=true</span>
              </el-descriptions-item>
              <el-descriptions-item label="积分换算">1 积分 = 0.01 元</el-descriptions-item>
            </el-descriptions>
          </el-card>
        </el-col>
      </el-row>

      <el-card v-if="models" shadow="never" body-style="padding:16px" style="margin-bottom: 12px">
        <template #header><span>可用模型</span></template>
        <el-tabs>
          <el-tab-pane v-for="g in MODEL_GROUPS" :key="g.key" :label="g.title">
            <el-table :data="models[g.key]" stripe border size="small" style="width: 100%">
              <el-table-column label="代号" min-width="150">
                <template #default="{ row }">
                  <span class="mono">{{ row.code }}</span>
                  <el-tag v-if="row.default" type="primary" size="small" style="margin-left: 6px">默认</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="version" label="版本" width="100" />
              <el-table-column prop="label" label="名称" min-width="160" />
              <el-table-column label="状态" width="110" align="center">
                <template #default="{ row }">
                  <el-tag :type="STATUS_TAG[row.status]?.type || 'info'" size="small">
                    {{ STATUS_TAG[row.status]?.label || row.status }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="note" label="说明" min-width="200" show-overflow-tooltip />
            </el-table>
          </el-tab-pane>
        </el-tabs>
        <el-alert type="info" :closable="false" show-icon style="margin-top: 12px">
          上游发布新模型时，无需改代码：在 backend/.env 里配置
          <code>SUNO_EXTRA_GENERATE_MODELS=代号|版本|展示名|说明</code>（多个用逗号分隔）后重启服务即可。
        </el-alert>
      </el-card>

      <el-card shadow="never" body-style="padding:16px">
        <template #header><span>接口定价</span></template>
        <el-table :data="system.prices" stripe border size="small" style="width: 100%">
          <el-table-column prop="label" label="接口" min-width="160" />
          <el-table-column label="标识" min-width="150">
            <template #default="{ row }">
              <span class="mono">{{ row.kind }}</span>
            </template>
          </el-table-column>
          <el-table-column label="消耗积分" width="120" align="right">
            <template #default="{ row }">{{ row.points }}</template>
          </el-table-column>
          <el-table-column label="折合金额" width="120" align="right">
            <template #default="{ row }">
              <span class="mono">{{ toAmount(row.points) }}</span>
            </template>
          </el-table-column>
        </el-table>

        <el-alert type="info" :closable="false" show-icon style="margin-top: 12px">
          生成音乐与 Remaster 一次产出两条任务，费用平摊到两条记录。生成失败不退积分，可免费重试；
          仅当上游当场拒收、任务未建立时立即退还。
          查询类接口（任务查询、批量查询、余额、流水）不消耗积分。
        </el-alert>
      </el-card>
    </template>
  </div>
</template>
