<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { getJSON, type Usage } from '../lib/api'
import { seriesHitRates, seriesTokens, seriesTotals } from '../lib/metrics'
import { fmtInt, fmtPct, fmtUSD } from '../lib/format'
import { useChartTheme } from '../charts'

const { t, locale } = useI18n()
const chartTheme = useChartTheme()
const days = ref(30)
const bucket = ref<'hour' | 'day' | 'week'>('day')
const groupBy = ref<'model' | 'provider' | 'client'>('model')
const usage = ref<Usage | null>(null)
const error = ref('')

// The router cuts buckets in UTC, so from is a UTC date.
function fromDate(n: number) {
  const d = new Date()
  d.setUTCDate(d.getUTCDate() - (n - 1))
  return d.toISOString().slice(0, 10)
}

async function load() {
  try {
    usage.value = await getJSON<Usage>(`/api/usage?from=${fromDate(days.value)}&bucket=${bucket.value}&group_by=${groupBy.value}`)
    error.value = ''
  } catch (e) {
    error.value = (e as Error).message
  }
}

watch([days, bucket, groupBy], load, { immediate: true })

const series = computed(() => usage.value?.series ?? [])
const base = computed(() => ({
  backgroundColor: 'transparent',
  tooltip: { trigger: 'axis', valueFormatter: (v: number | null) => (v === null ? '–' : fmtInt(v, locale.value)) },
  legend: { type: 'scroll', top: 0 },
  grid: { left: 70, right: 20, top: 50, bottom: 30 },
}))

const tokenChart = computed(() => ({
  ...base.value,
  xAxis: { type: 'category', data: usage.value?.buckets ?? [] },
  yAxis: { type: 'value', name: t('usage.tokens') },
  series: series.value.map((s) => ({ name: s.key || '–', type: 'bar', stack: 'tokens', itemStyle: { borderRadius: 2 }, data: seriesTokens(s) })),
}))

const hitChart = computed(() => ({
  ...base.value,
  xAxis: { type: 'category', data: usage.value?.buckets ?? [] },
  yAxis: { type: 'value', min: 0, max: 100, axisLabel: { formatter: '{value} %' } },
  series: series.value.map((s) => ({
    name: s.key || '–', type: 'line', tooltip: { valueFormatter: (v: number | null) => (v === null ? '–' : `${v} %`) }, smooth: true, symbolSize: 4, connectNulls: false,
    data: seriesHitRates(s).map((v) => (v === null ? null : Math.round(v * 1000) / 10)),
  })),
}))

const totals = computed(() => series.value.map((s) => ({ key: s.key || '–', billed: s.billed, ...seriesTotals(s) })))

const costChart = computed(() => ({
  backgroundColor: 'transparent',
  tooltip: { trigger: 'item', valueFormatter: (v: number) => fmtUSD(v, locale.value) },
  legend: { type: 'scroll', orient: 'vertical', right: 0, top: 'middle' },
  series: [{
    type: 'pie', radius: ['50%', '75%'], center: ['35%', '50%'], itemStyle: { borderRadius: 4 },
    label: { show: false },
    data: totals.value.filter((r) => r.costUSD > 0).map((r) => ({ name: r.key, value: r.costUSD })),
  }],
}))
</script>

<template>
  <section>
    <div class="toolbar">
      <label>{{ t('usage.range') }}
        <select v-model.number="days">
          <option v-for="n in [7, 30, 90]" :key="n" :value="n">{{ t('usage.days', { n }) }}</option>
        </select>
      </label>
      <label>{{ t('usage.bucket') }}
        <select v-model="bucket">
          <option value="hour">{{ t('usage.hour') }}</option>
          <option value="day">{{ t('usage.day') }}</option>
          <option value="week">{{ t('usage.week') }}</option>
        </select>
      </label>
      <label>{{ t('usage.groupBy') }}
        <select v-model="groupBy">
          <option value="model">{{ t('usage.model') }}</option>
          <option value="provider">{{ t('usage.provider') }}</option>
          <option value="client">{{ t('usage.client') }}</option>
        </select>
      </label>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="usage && series.length === 0" class="note">{{ t('usage.empty') }}</p>

    <template v-if="series.length">
      <h3>{{ t('usage.tokens') }}</h3>
      <v-chart class="chart" :option="tokenChart" :theme="chartTheme" autoresize />
      <h3>{{ t('usage.hitRate') }}</h3>
      <v-chart class="chart" :option="hitChart" :theme="chartTheme" autoresize />

      <h3>{{ t('usage.totals') }}</h3>
      <div class="split">
        <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{{ t('col.series') }}</th><th class="num">{{ t('col.requests') }}</th><th class="num">{{ t('col.in') }}</th>
              <th class="num">{{ t('col.out') }}</th><th class="num">{{ t('col.cacheRW') }}</th><th class="num">{{ t('col.hit') }}</th>
              <th class="num">{{ t('col.cost') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in totals" :key="r.key + r.billed">
              <td>{{ r.key }}</td>
              <td class="num">{{ fmtInt(r.requests, locale) }}</td>
              <td class="num">{{ fmtInt(r.tokensIn, locale) }}</td>
              <td class="num">{{ fmtInt(r.tokensOut, locale) }}</td>
              <td class="num">{{ fmtInt(r.cacheRead, locale) }} / {{ fmtInt(r.cacheWrite, locale) }}</td>
              <td class="num">{{ fmtPct(r.hitRate, locale) }}</td>
              <td class="num">{{ fmtUSD(r.costUSD, locale) }}<span v-if="!r.billed" class="note"> ({{ t('usage.listPrice') }})</span></td>
            </tr>
          </tbody>
        </table>
        </div>
        <v-chart class="chart" :option="costChart" :theme="chartTheme" autoresize />
      </div>
    </template>
  </section>
</template>
