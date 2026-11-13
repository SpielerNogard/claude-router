<script setup lang="ts">
import { computed } from 'vue'
import VChart from 'vue-echarts'
import { PALETTE } from '../charts'

const props = defineProps<{ data: (number | null)[] }>()

const option = computed(() => ({
  animation: false,
  grid: { left: 0, right: 0, top: 2, bottom: 2 },
  xAxis: { type: 'category', show: false, data: props.data.map((_, i) => i) },
  yAxis: { type: 'value', show: false, min: 0 },
  series: [{
    type: 'line', data: props.data, smooth: true, showSymbol: false, connectNulls: false,
    lineStyle: { width: 1.5, color: PALETTE[0] }, areaStyle: { color: PALETTE[0], opacity: 0.12 },
  }],
}))
</script>

<template>
  <v-chart class="spark" :option="option" autoresize />
</template>
