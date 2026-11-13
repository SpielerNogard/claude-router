<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import VChart from 'vue-echarts'
import { getJSON, type Usage } from '../lib/api'
import { dailyPoints, weekdayHourGrid, type Metric } from '../lib/activity'
import { fmtInt, fmtUSD } from '../lib/format'
import { useChartTheme, useDark } from '../charts'

const { t, locale } = useI18n()
const chartTheme = useChartTheme()
const dark = useDark()
const metric = ref<Metric>('requests')
const daily = ref<Usage | null>(null)
const hourly = ref<Usage | null>(null)
const error = ref('')

// Buckets are UTC days, so from is a UTC date.
function utcDaysAgo(n: number) {
  const d = new Date()
  d.setUTCDate(d.getUTCDate() - n)
  return d.toISOString().slice(0, 10)
}

Promise.allSettled([
  getJSON<Usage>(`/api/usage?from=${utcDaysAgo(364)}&bucket=day`),
  // 8 weeks: the router's maxBuckets allows at most 2000 hourly buckets.
  getJSON<Usage>(`/api/usage?from=${utcDaysAgo(55)}&bucket=hour`),
]).then(([d, h]) => {
  if (d.status === 'fulfilled') daily.value = d.value
  if (h.status === 'fulfilled') hourly.value = h.value
  const failed = [d, h].find((r) => r.status === 'rejected')
  if (failed) error.value = (failed.reason as Error).message
})

const fmt = (v: number) => (metric.value === 'cost' ? fmtUSD(v, locale.value) : fmtInt(v, locale.value))
const ramp = computed(() => (dark.value ? ['#1f2430', '#7b9cff'] : ['#e8edf9', '#3867d6']))
// Gaps in the card color; otherwise ECharts fills calendar cells white.
const gap = computed(() => (dark.value ? '#18181d' : '#ffffff'))
// Empty cells are absent from the data and show the grey background; otherwise visualMap clamps 0 onto the ramp.
const empty = computed(() => (dark.value ? '#2a2a31' : '#e5e5ea'))
const scale = (max: number, seriesIndex = 0) => ({ show: false, min: 0, max, seriesIndex, inRange: { color: ramp.value } })
const maxOf = (xs: number[]) => Math.max(1e-9, ...xs)

// 2024-01-01 was a Monday.
const weekdayNames = computed(() =>
  Array.from({ length: 7 }, (_, i) =>
    new Date(Date.UTC(2024, 0, 1 + i)).toLocaleDateString(locale.value, { weekday: 'short', timeZone: 'UTC' })),
)
const monthNames = computed(() =>
  Array.from({ length: 12 }, (_, i) => new Date(Date.UTC(2024, i, 1)).toLocaleDateString(locale.value, { month: 'short', timeZone: 'UTC' })),
)

const calendar = computed(() => {
  const pts = daily.value ? dailyPoints(daily.value, metric.value) : []
  return {
    tooltip: { formatter: (p: { value: [string, number] }) => `${p.value[0]}: ${fmt(p.value[1])}` },
    visualMap: scale(maxOf(pts.map((p) => p[1]))),
    calendar: {
      top: 30, left: 40, right: 10, cellSize: ['auto', 14],
      range: pts.length ? [pts[0][0], pts[pts.length - 1][0]] : utcDaysAgo(0),
      splitLine: { show: false }, itemStyle: { color: empty.value, borderWidth: 3, borderColor: gap.value },
      // ECharts' nameMap starts on Sunday.
      dayLabel: { firstDay: 1, nameMap: [weekdayNames.value[6], ...weekdayNames.value.slice(0, 6)] },
      monthLabel: { nameMap: monthNames.value }, yearLabel: { show: false },
    },
    series: [{ type: 'heatmap', coordinateSystem: 'calendar', data: pts.filter((p) => p[1] > 0), itemStyle: { borderWidth: 3, borderColor: gap.value } }],
  }
})

const weekHour = computed(() => {
  const cells = hourly.value ? weekdayHourGrid(hourly.value, metric.value) : []
  return {
    tooltip: { formatter: (p: { value: [number, number, number] }) => `${weekdayNames.value[p.value[1]]} ${p.value[0]}:00 – ${fmt(p.value[2])}` },
    grid: { left: 50, right: 10, top: 10, bottom: 30 },
    xAxis: { type: 'category', data: Array.from({ length: 24 }, (_, h) => String(h)), splitArea: { show: false } },
    yAxis: { type: 'category', data: weekdayNames.value, inverse: true },
    visualMap: scale(maxOf(cells.map((c) => c[2])), 1),
    series: [
      {
        type: 'heatmap', silent: true, tooltip: { show: false }, data: cells.map(([h, d]) => [h, d, 0]),
        itemStyle: { color: empty.value, borderRadius: 3, borderColor: gap.value, borderWidth: 2 },
      },
      { type: 'heatmap', data: cells.filter((c) => c[2] > 0), itemStyle: { borderRadius: 3, borderColor: gap.value, borderWidth: 2 } },
    ],
  }
})
</script>

<template>
  <section>
    <div class="toolbar">
      <span class="segmented">
        <button v-for="m in (['requests', 'tokens', 'cost'] as const)" :key="m" :class="{ active: metric === m }" @click="metric = m">
          {{ t(`activity.${m}`) }}
        </button>
      </span>
    </div>
    <p v-if="error" class="error">{{ error }}</p>
    <h3>{{ t('activity.year') }}</h3>
    <v-chart class="chart heat-year" :option="calendar" :theme="chartTheme" autoresize />
    <h3>{{ t('activity.weekHour') }}</h3>
    <v-chart class="chart heat-week" :option="weekHour" :theme="chartTheme" autoresize />
  </section>
</template>
