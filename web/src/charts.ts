import { computed, ref } from 'vue'
import { registerTheme, use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, HeatmapChart, LineChart, PieChart } from 'echarts/charts'
import { CalendarComponent, GridComponent, LegendComponent, TooltipComponent, VisualMapComponent } from 'echarts/components'
import { parseTheme, type Theme } from './lib/theme'

use([
  CanvasRenderer, BarChart, LineChart, PieChart, HeatmapChart,
  GridComponent, LegendComponent, TooltipComponent, CalendarComponent, VisualMapComponent,
])

const THEME_KEY = 'router-theme'

function storedTheme(): Theme {
  try {
    return parseTheme(localStorage.getItem(THEME_KEY))
  } catch {
    return 'auto'
  }
}

export const theme = ref<Theme>(storedTheme())
const mq = window.matchMedia('(prefers-color-scheme: dark)')
const systemDark = ref(mq.matches)
mq.addEventListener('change', (e) => (systemDark.value = e.matches))
const isDark = computed(() => theme.value === 'dark' || (theme.value === 'auto' && systemDark.value))

// "auto" removes the attribute so the CSS media query applies again.
function applyTheme(t: Theme) {
  if (t === 'auto') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = t
}
applyTheme(theme.value)

export function setTheme(t: Theme) {
  theme.value = t
  applyTheme(t)
  try {
    localStorage.setItem(THEME_KEY, t)
  } catch {
    /* choice then only lasts until reload */
  }
}

export const useDark = () => isDark

export const PALETTE = ['#3867d6', '#20bf6b', '#f7b731', '#eb3b5a', '#8854d0', '#0fb9b1', '#fa8231', '#778ca3']

function routerTheme(fg: string, muted: string, line: string) {
  const axis = {
    axisLine: { lineStyle: { color: line } },
    axisTick: { lineStyle: { color: line } },
    axisLabel: { color: muted },
    splitLine: { lineStyle: { color: line } },
    nameTextStyle: { color: muted },
  }
  return {
    color: PALETTE,
    backgroundColor: 'transparent',
    textStyle: { fontFamily: 'system-ui, sans-serif', color: fg },
    legend: { textStyle: { color: fg } },
    categoryAxis: axis,
    valueAxis: axis,
  }
}
registerTheme('router-light', routerTheme('#1d1d1f', '#6e6e73', '#e5e5ea'))
registerTheme('router-dark', routerTheme('#ececf1', '#9a9aa3', '#2a2a31'))

export const useChartTheme = () => computed(() => (isDark.value ? 'router-dark' : 'router-light'))
