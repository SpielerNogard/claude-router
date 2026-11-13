<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { getJSON, type RequestRow, type Stats } from '../lib/api'
import { hitRate } from '../lib/metrics'
import type { ProviderLimit } from '../lib/limits'
import { RANGES, pageCount, providerTiles, rangeQuery, statsSeries, type RangeKey } from '../lib/live'
import { usePoll } from '../lib/poll'
import { fmtCompact, fmtInt, fmtMs, fmtPct, fmtTime, fmtUSD } from '../lib/format'
import { useChartTheme } from '../charts'
import Sparkline from './Sparkline.vue'
import RequestDocs from './RequestDocs.vue'

const PAGE_SIZE = 50
const RANGE_KEY = 'router-range'

const { t, locale } = useI18n()
const chartTheme = useChartTheme()

function storedRange(): RangeKey {
  try {
    const r = localStorage.getItem(RANGE_KEY)
    return RANGES.some((x) => x.key === r) ? (r as RangeKey) : '1h'
  } catch {
    return '1h'
  }
}

const range = ref<RangeKey>(storedRange())
const stats = ref<Stats | null>(null)
const limits = ref<Record<string, ProviderLimit>>({})
const rows = ref<RequestRow[]>([])
const total = ref(0)
const page = ref(1)
const paused = ref(false)
const pausedTotal = ref(0)
const error = ref('')
const now = ref(new Date())
const provider = ref('')
const model = ref('')
const errorsOnly = ref(false)
const open = ref(new Set<number>())

function toggle(id: number) {
  const s = new Set(open.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  open.value = s
}

function requestsQuery(offset: number, limit: number) {
  const p = new URLSearchParams({ since: rangeQuery(range.value, now.value).since, limit: String(limit), offset: String(offset) })
  if (provider.value) p.set('provider', provider.value)
  if (model.value) p.set('model', model.value)
  if (errorsOnly.value) p.set('errors', '1')
  return `/api/requests?${p}`
}

async function loadPage() {
  const res = await getJSON<{ entries: RequestRow[]; total: number }>(requestsQuery((page.value - 1) * PAGE_SIZE, PAGE_SIZE))
  rows.value = res.entries
  total.value = res.total
}

async function refresh() {
  now.value = new Date()
  try {
    const q = rangeQuery(range.value, now.value)
    stats.value = await getJSON<Stats>(`/api/stats?since=${q.since}&step=${q.step}`)
    if (paused.value) {
      total.value = (await getJSON<{ total: number }>(requestsQuery(0, 1))).total
    } else if (page.value === 1) {
      await loadPage()
    }
    error.value = ''
  } catch (e) {
    error.value = (e as Error).message
    throw e
  }
}

usePoll(refresh, 2000)
usePoll(async () => {
  limits.value = await getJSON<Record<string, ProviderLimit>>('/api/limits')
}, 10_000)

watch([range, provider, model, errorsOnly], () => {
  try {
    localStorage.setItem(RANGE_KEY, range.value)
  } catch {
    /* choice then only lasts until reload */
  }
  page.value = 1
  paused.value = false
  open.value = new Set()
  refresh().catch(() => {})
})
watch(page, () => loadPage().catch((e) => (error.value = (e as Error).message)))

const pages = computed(() => pageCount(total.value, PAGE_SIZE))
const newCount = computed(() => (paused.value ? total.value - pausedTotal.value : 0))
const tiles = computed(() => providerTiles(limits.value, stats.value?.by_provider ?? {}, now.value, locale.value))
const k = computed(() => stats.value?.totals)
const spark = computed(() => (stats.value ? statsSeries(stats.value) : null))

function togglePause() {
  paused.value = !paused.value
  pausedTotal.value = total.value
  if (!paused.value) refresh().catch(() => {})
}

function clearFilter() {
  provider.value = ''
  model.value = ''
  errorsOnly.value = false
}

const chart = computed(() => {
  const s = stats.value ? statsSeries(stats.value) : { times: [], tokens: [], hitRate: [] }
  const longRange = range.value === '24h' || range.value === '7d'
  const label = (d: Date) =>
    longRange
      ? d.toLocaleString(locale.value, { weekday: 'short', hour: '2-digit', minute: '2-digit' })
      : d.toLocaleTimeString(locale.value, { hour: '2-digit', minute: '2-digit' })
  return {
    backgroundColor: 'transparent',
    // New data every 2 s: without this every poll would re-animate the bars.
    animation: false,
    tooltip: { trigger: 'axis', valueFormatter: (v: number | null) => (v === null ? '–' : fmtInt(v, locale.value)) },
    legend: { top: 0 },
    grid: { left: 70, right: 50, top: 40, bottom: 30 },
    xAxis: { type: 'category', data: s.times.map(label) },
    yAxis: [
      { type: 'value', name: 'tokens' },
      { type: 'value', min: 0, max: 100, axisLabel: { formatter: '{value} %' } },
    ],
    series: [
      { name: t('usage.tokens'), type: 'bar', itemStyle: { borderRadius: [3, 3, 0, 0] }, data: s.tokens },
      { name: t('live.hitRate'), type: 'line', yAxisIndex: 1, tooltip: { valueFormatter: (v: number | null) => (v === null ? '–' : `${v} %`) }, smooth: true, symbolSize: 4, connectNulls: false, data: s.hitRate.map((v) => (v === null ? null : Math.round(v * 1000) / 10)) },
    ],
  }
})
</script>

<template>
  <section>
    <div class="toolbar">
      <span class="segmented">
        <button v-for="r in RANGES" :key="r.key" :class="{ active: range === r.key }" @click="range = r.key">
          {{ t(`live.range.${r.key}`) }}
        </button>
      </span>
    </div>
    <p v-if="error" class="error">{{ error }}</p>

    <div v-if="k" class="kpis">
      <div class="kpi"><div class="label">{{ t('live.requests') }}</div><div class="value">{{ fmtInt(k.requests, locale) }}</div></div>
      <div class="kpi"><div class="label">{{ t('live.tokensIn') }}</div><div class="value">{{ fmtInt(k.input_tokens, locale) }}</div><div class="kpi-sub">{{ t('live.cacheRead', { n: fmtCompact(k.cache_read, locale) }) }}<br />{{ t('live.cacheWrite', { n: fmtCompact(k.cache_write, locale) }) }}</div><Sparkline v-if="stats" :data="stats.series.input_tokens" /></div>
      <div class="kpi"><div class="label">{{ t('live.tokensOut') }}</div><div class="value">{{ fmtInt(k.output_tokens, locale) }}</div><Sparkline v-if="stats" :data="stats.series.output_tokens" /></div>
      <div class="kpi"><div class="label">{{ t('live.hitRate') }}</div><div class="value">{{ fmtPct(hitRate(k.input_tokens, k.cache_read, k.cache_write), locale) }}</div><Sparkline v-if="spark" :data="spark.hitRate" /></div>
      <div class="kpi"><div class="label">{{ t('live.cost') }}</div><div class="value">{{ fmtUSD(k.cost_usd, locale) }}</div></div>
    </div>

    <div class="tiles">
      <div v-for="p in tiles" :key="p.name" class="tile">
        <div class="tile-head">
          <strong>{{ p.name }}</strong>
          <span v-if="p.unlimited" class="note" :title="t('limits.unlimited')">∞ {{ t('limits.unlimited') }}</span>
          <span v-else-if="p.windows.length === 0" class="note">– {{ t('limits.noData') }}</span>
        </div>
        <div
          v-for="w in p.windows"
          :key="w.name"
          :class="['win', w.level]"
          :title="w.level === 'stale' ? t('limits.stale') : t('limits.resets', { time: w.reset })"
        >
          <span class="win-name">{{ w.name }}</span>
          <span class="bar"><span :style="{ width: Math.min(w.pct, 1) * 100 + '%' }" /></span>
          <span class="win-pct">{{ w.level === 'stale' ? '–' : fmtPct(w.pct, locale) }}</span>
          <span class="note">{{ t('limits.resets', { time: w.reset }) }}</span>
        </div>
        <dl class="tile-usage">
          <div><dt>{{ t('live.requests') }}</dt><dd>{{ fmtInt(p.totals.requests, locale) }}</dd></div>
          <div><dt>{{ t('live.tokensIn') }}</dt><dd>{{ fmtInt(p.totals.input_tokens, locale) }}</dd></div>
          <div><dt>{{ t('live.tokensOut') }}</dt><dd>{{ fmtInt(p.totals.output_tokens, locale) }}</dd></div>
          <div><dt>{{ t('live.hitRate') }}</dt><dd>{{ fmtPct(p.hitRate, locale) }}</dd></div>
          <div><dt>{{ t('live.cost') }}</dt><dd>{{ fmtUSD(p.totals.cost_usd, locale) }}</dd></div>
        </dl>
      </div>
    </div>

    <v-chart class="chart" :option="chart" :theme="chartTheme" autoresize />

    <div class="toolbar">
      <button :disabled="page !== 1" @click="togglePause">{{ paused ? t('live.resume') : t('live.pause') }}</button>
      <span v-if="paused && newCount > 0">{{ t('live.newCount', { n: newCount }) }}</span>
      <label><input v-model="errorsOnly" type="checkbox" /> {{ t('live.errorsOnly') }}</label>
      <button v-if="provider || model || errorsOnly" @click="clearFilter">{{ t('live.clearFilter') }}</button>
      <span class="pager">
        <button :disabled="page <= 1" @click="page--">‹ {{ t('live.newer') }}</button>
        {{ t('live.page', { page, pages }) }}
        <button :disabled="page >= pages" @click="page++">{{ t('live.older') }} ›</button>
      </span>
    </div>

    <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th></th><th>{{ t('col.time') }}</th><th>{{ t('col.client') }}</th><th>{{ t('col.provider') }}</th><th>{{ t('col.model') }}</th>
          <th class="num">{{ t('col.in') }}</th><th class="num">{{ t('col.out') }}</th><th class="num">{{ t('col.cacheRW') }}</th>
          <th class="num">{{ t('col.hit') }}</th><th class="num">{{ t('col.cost') }}</th><th class="num">{{ t('col.duration') }}</th>
          <th>{{ t('col.status') }}</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="r in rows" :key="r.id">
          <tr :class="{ error: r.status !== 'ok', expandable: true }" @click="toggle(r.id)">
            <td class="chev"><button :aria-expanded="open.has(r.id)" :aria-label="t('docs.toggle')" @click.stop="toggle(r.id)">{{ open.has(r.id) ? '▾' : '▸' }}</button></td>
            <td>{{ fmtTime(r.ts, locale) }}</td>
            <td>{{ r.client }}</td>
            <td><span class="link" @click.stop="provider = r.provider">{{ r.provider }}</span></td>
            <td><span class="link" @click.stop="model = r.model">{{ r.model }}</span></td>
            <td class="num">{{ fmtInt(r.input_tokens, locale) }}</td>
            <td class="num">{{ fmtInt(r.output_tokens, locale) }}</td>
            <td class="num">{{ fmtInt(r.cache_read, locale) }} / {{ fmtInt(r.cache_write, locale) }}</td>
            <td class="num">{{ fmtPct(hitRate(r.input_tokens, r.cache_read, r.cache_write), locale) }}</td>
            <td class="num">{{ fmtUSD(r.cost_usd, locale) }}</td>
            <td class="num">{{ fmtMs(r.duration_ms, locale) }}</td>
            <td><span :class="['badge', r.status]">{{ r.status }}</span><template v-if="r.error_code"> {{ r.error_code }}</template></td>
          </tr>
          <tr v-if="open.has(r.id)" class="detail">
            <td colspan="12"><RequestDocs :request-id="r.request_id" /></td>
          </tr>
        </template>
      </tbody>
    </table>
    </div>
  </section>
</template>
