<script setup>
import { computed, onActivated, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { fetchKindStats, fetchOverview, fetchTrend, fetchUpstream, refreshUpstream } from '@/api'
import { formatTime, thousands, toAmount } from '@/utils/format'

echarts.use([LineChart, BarChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

/**
 * 配色取自校验通过的分类色板（蓝 / 青 / 橙）：
 * 相邻色对的色盲可分辨度与常视辨识度均达标；
 * 青色在白底上对比度略低，因此每条折线都带末端直标作为补充识别。
 */
const SERIES_COLORS = { total: '#2a78d6', completed: '#1baf7a', failed: '#eb6834' }
const AXIS_COLOR = '#909399'
const SPLIT_COLOR = '#ebeef5'

const loading = ref(false)
const overview = ref(null)
const trendDays = ref(7)
const trend = ref([])
const kindStats = ref([])

// 上游账户余额：上游没有 webhook，这里展示的是后端定时轮询的结果
const upstream = ref(null)
const upstreamRefreshing = ref(false)

const upstreamState = computed(() => {
  const snap = upstream.value?.snapshot
  if (!snap) return null
  if (!snap.supported) return { type: 'info', text: '当前使用本地 Mock，无上游账户' }
  if (snap.error) return { type: 'danger', text: `查询失败：${snap.error}` }
  if (snap.low) return { type: 'danger', text: `余额低于告警阈值 ${snap.threshold}，请尽快充值` }
  return { type: 'success', text: '余额充足' }
})

async function loadUpstream() {
  upstream.value = await fetchUpstream()
}

async function handleRefreshUpstream() {
  upstreamRefreshing.value = true
  try {
    const data = await refreshUpstream()
    if (upstream.value) upstream.value.snapshot = data.snapshot
    else upstream.value = data
  } finally {
    upstreamRefreshing.value = false
  }
}

const trendEl = ref()
const pointsEl = ref()
const kindEl = ref()
const charts = shallowRef([])

const cards = computed(() => {
  const o = overview.value
  if (!o) return []
  return [
    { label: '商户总数', value: thousands(o.merchant_total), extra: `启用中 ${o.merchant_active}`, color: '#2a78d6' },
    { label: '启用中的密钥', value: thousands(o.key_total), extra: '仅统计未禁用', color: '#4a3aa7' },
    { label: '今日任务', value: thousands(o.task_today), extra: `累计 ${thousands(o.task_total)}`, color: '#1baf7a' },
    { label: '进行中任务', value: thousands(o.task_running), extra: `失败 ${thousands(o.task_failed)}`, color: '#eb6834' },
    { label: '今日消耗积分', value: thousands(o.consumed_today), extra: toAmount(o.consumed_today), color: '#2a78d6' },
    { label: '累计消耗积分', value: thousands(o.consumed_total), extra: toAmount(o.consumed_total), color: '#4a3aa7' },
    { label: '商户余额合计', value: thousands(o.points_remaining), extra: toAmount(o.points_remaining), color: '#1baf7a' },
    { label: '累计退还积分', value: thousands(o.refunded_total), extra: `充值 ${thousands(o.recharged_total)}`, color: '#eb6834' }
  ]
})

const successRate = computed(() => {
  const o = overview.value
  if (!o || !o.task_total) return '—'
  return `${((o.task_completed / o.task_total) * 100).toFixed(1)}%`
})

/* ---------------------------------- 图表 ---------------------------------- */

const baseGrid = { left: 48, right: 24, top: 36, bottom: 32 }

function axisLabel() {
  return { color: AXIS_COLOR, fontSize: 11 }
}

function renderTrend() {
  if (!trendEl.value) return
  const chart = echarts.init(trendEl.value)
  const dates = trend.value.map((p) => p.date.slice(5))

  const series = [
    { key: 'total', name: '总任务', color: SERIES_COLORS.total },
    { key: 'completed', name: '已完成', color: SERIES_COLORS.completed },
    { key: 'failed', name: '已失败', color: SERIES_COLORS.failed }
  ].map((s) => ({
    name: s.name,
    type: 'line',
    smooth: false,
    symbolSize: 8,
    lineStyle: { width: 2, color: s.color },
    itemStyle: { color: s.color, borderColor: '#fff', borderWidth: 2 },
    // 末端直标：identity 不只依赖颜色
    endLabel: { show: true, color: s.color, fontSize: 11, formatter: `{a}` },
    data: trend.value.map((p) => p[s.key])
  }))

  chart.setOption({
    color: Object.values(SERIES_COLORS),
    tooltip: { trigger: 'axis', axisPointer: { type: 'line', lineStyle: { color: '#c0c4cc' } } },
    legend: { data: ['总任务', '已完成', '已失败'], top: 0, right: 0, itemWidth: 14, itemHeight: 2 },
    grid: { ...baseGrid, right: 64 },
    xAxis: {
      type: 'category',
      data: dates,
      boundaryGap: false,
      axisLine: { lineStyle: { color: SPLIT_COLOR } },
      axisTick: { show: false },
      axisLabel: axisLabel()
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: axisLabel(),
      splitLine: { lineStyle: { color: SPLIT_COLOR } }
    },
    series
  })
  charts.value.push(chart)
}

function renderPoints() {
  if (!pointsEl.value) return
  const chart = echarts.init(pointsEl.value)

  // 单系列：不需要图例，标题已说明含义
  chart.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'line', lineStyle: { color: '#c0c4cc' } },
      valueFormatter: (v) => `${thousands(v)} 积分`
    },
    grid: baseGrid,
    xAxis: {
      type: 'category',
      data: trend.value.map((p) => p.date.slice(5)),
      boundaryGap: false,
      axisLine: { lineStyle: { color: SPLIT_COLOR } },
      axisTick: { show: false },
      axisLabel: axisLabel()
    },
    yAxis: {
      type: 'value',
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: axisLabel(),
      splitLine: { lineStyle: { color: SPLIT_COLOR } }
    },
    series: [
      {
        name: '消耗积分',
        type: 'line',
        smooth: false,
        symbolSize: 8,
        lineStyle: { width: 2, color: SERIES_COLORS.total },
        itemStyle: { color: SERIES_COLORS.total, borderColor: '#fff', borderWidth: 2 },
        areaStyle: {
          color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: 'rgba(42,120,214,0.18)' },
            { offset: 1, color: 'rgba(42,120,214,0.01)' }
          ])
        },
        data: trend.value.map((p) => p.consumed)
      }
    ]
  })
  charts.value.push(chart)
}

function renderKinds() {
  if (!kindEl.value) return
  const chart = echarts.init(kindEl.value)

  // 排名比较用条形图而非饼图：长度比角度更易读，也便于直接标数值
  const sorted = [...kindStats.value].sort((a, b) => a.count - b.count)

  chart.setOption({
    tooltip: {
      trigger: 'item',
      formatter: (p) => {
        const item = sorted[p.dataIndex]
        return `${item.label}<br/>调用 ${thousands(item.count)} 次<br/>消耗 ${thousands(item.consumed)} 积分`
      }
    },
    grid: { left: 110, right: 56, top: 12, bottom: 12 },
    xAxis: { type: 'value', show: false },
    yAxis: {
      type: 'category',
      data: sorted.map((s) => s.label),
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: axisLabel()
    },
    series: [
      {
        type: 'bar',
        barWidth: 12,
        // 数据末端 4px 圆角，贴合基线
        itemStyle: { color: SERIES_COLORS.total, borderRadius: [0, 4, 4, 0] },
        label: { show: true, position: 'right', color: '#606266', fontSize: 11 },
        data: sorted.map((s) => s.count)
      }
    ]
  })
  charts.value.push(chart)
}

function disposeCharts() {
  charts.value.forEach((c) => c.dispose())
  charts.value = []
}

function renderAll() {
  disposeCharts()
  renderTrend()
  renderPoints()
  renderKinds()
}

function handleResize() {
  charts.value.forEach((c) => c.resize())
}

/* ---------------------------------- 数据 ---------------------------------- */

async function load() {
  loading.value = true
  try {
    const [o, t, k] = await Promise.all([
      fetchOverview(),
      fetchTrend(trendDays.value),
      fetchKindStats(30)
    ])
    loadUpstream().catch(() => {})
    overview.value = o
    trend.value = t || []
    kindStats.value = k || []
    await nextTickRender()
  } finally {
    loading.value = false
  }
}

function nextTickRender() {
  return new Promise((resolve) => {
    requestAnimationFrame(() => {
      renderAll()
      resolve()
    })
  })
}

watch(trendDays, load)

onMounted(() => {
  load()
  window.addEventListener('resize', handleResize)
})

onActivated(handleResize)

onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  disposeCharts()
})
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div>
        <h2>数据概览</h2>
        <p class="desc">任务成功率 {{ successRate }}，数据实时取自数据库</p>
      </div>
      <el-button :icon="'Refresh'" @click="load">刷新</el-button>
    </div>

    <el-card
      v-if="upstream"
      shadow="never"
      class="upstream-bar"
      body-style="padding:14px 16px"
      style="margin-bottom: 12px"
    >
      <div class="upstream-row">
        <div class="left">
          <span class="title">上游账户余额</span>
          <span v-if="upstream.snapshot?.supported" class="value">
            {{ thousands(upstream.snapshot.balance) }}
          </span>
          <span v-else class="value muted">—</span>
          <el-tag v-if="upstreamState" :type="upstreamState.type" size="small">
            {{ upstreamState.text }}
          </el-tag>
        </div>

        <div class="right">
          <span class="text-muted" style="font-size: 12px">
            <template v-if="upstream.snapshot?.checked_at">
              更新于 {{ formatTime(upstream.snapshot.checked_at) }}，每 {{ upstream.interval }} 自动刷新
            </template>
            <template v-else>尚未查询</template>
          </span>
          <el-button size="small" :loading="upstreamRefreshing" @click="handleRefreshUpstream">
            立即刷新
          </el-button>
        </div>
      </div>
      <p class="upstream-tip">
        上游未提供充值回调（webhook），此处为定时轮询结果；在 open.suno.cn 充值后点「立即刷新」可马上看到。
      </p>
    </el-card>

    <el-row :gutter="12">
      <el-col v-for="card in cards" :key="card.label" :xs="12" :sm="12" :md="6" :lg="6" style="margin-bottom: 12px">
        <el-card shadow="never" class="stat-card" body-style="padding:16px">
          <div class="value" :style="{ color: card.color }">{{ card.value }}</div>
          <div class="label">{{ card.label }}</div>
          <div class="extra">{{ card.extra }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="12">
      <el-col :xs="24" :lg="16" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header>
            <div class="card-head">
              <span>任务趋势</span>
              <el-radio-group v-model="trendDays" size="small">
                <el-radio-button :value="7">近 7 天</el-radio-button>
                <el-radio-button :value="14">近 14 天</el-radio-button>
                <el-radio-button :value="30">近 30 天</el-radio-button>
              </el-radio-group>
            </div>
          </template>
          <div ref="trendEl" style="height: 280px"></div>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="8" style="margin-bottom: 12px">
        <el-card shadow="never" body-style="padding:16px">
          <template #header><span>任务类型分布（近 30 天）</span></template>
          <div ref="kindEl" style="height: 280px"></div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" body-style="padding:16px">
      <template #header><span>积分消耗趋势</span></template>
      <div ref="pointsEl" style="height: 240px"></div>
    </el-card>
  </div>
</template>

<style scoped>
.upstream-bar .upstream-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}

.upstream-bar .left {
  display: flex;
  align-items: baseline;
  gap: 10px;
}

.upstream-bar .title {
  font-size: 14px;
  font-weight: 600;
}

.upstream-bar .value {
  font-size: 22px;
  font-weight: 700;
  font-family: 'JetBrains Mono', Consolas, monospace;
  color: #2a78d6;
}

.upstream-bar .value.muted {
  color: #c0c4cc;
}

.upstream-bar .right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.upstream-tip {
  margin: 8px 0 0;
  font-size: 12px;
  color: #909399;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
</style>
