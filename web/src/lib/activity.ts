import type { Usage } from './api'

export type Metric = 'requests' | 'tokens' | 'cost'

export const bucketTotals = (u: Usage, m: Metric): number[] =>
  u.buckets.map((_, i) =>
    u.series.reduce((sum, s) => {
      if (m === 'requests') return sum + s.requests[i]
      if (m === 'cost') return sum + s.cost_usd[i]
      return sum + s.tokens_in[i] + s.tokens_out[i] + s.cache_read[i] + s.cache_write[i]
    }, 0),
  )

export function dailyPoints(u: Usage, m: Metric): [string, number][] {
  const v = bucketTotals(u, m)
  return u.buckets.map((b, i) => [b, v[i]])
}

const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

// The router's hour labels are UTC ("2026-03-23T22"); Intl converts them, including DST.
export function weekdayHourGrid(u: Usage, m: Metric, timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone) {
  const fmt = new Intl.DateTimeFormat('en-US', { timeZone, weekday: 'short', hour: 'numeric', hourCycle: 'h23' })
  const cells = new Array<number>(7 * 24).fill(0)
  const v = bucketTotals(u, m)
  u.buckets.forEach((b, i) => {
    const parts = fmt.formatToParts(new Date(`${b}:00:00Z`))
    const day = WEEKDAYS.indexOf(parts.find((p) => p.type === 'weekday')!.value)
    const hour = Number(parts.find((p) => p.type === 'hour')!.value)
    cells[day * 24 + hour] += v[i]
  })
  return cells.map((val, idx): [number, number, number] => [idx % 24, Math.floor(idx / 24), val])
}
