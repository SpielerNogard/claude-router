import type { Stats, Totals } from './api'
import { WINDOWS, fmtReset, level, type ProviderLimit } from './limits'
import { hitRate } from './metrics'

export const RANGES = [
  { key: '1h', hours: 1, step: 60 },
  { key: '3h', hours: 3, step: 300 },
  { key: '6h', hours: 6, step: 600 },
  { key: '24h', hours: 24, step: 1800 },
  { key: '7d', hours: 168, step: 10800 },
] as const
export type RangeKey = (typeof RANGES)[number]['key']

export function rangeQuery(key: RangeKey, now: Date) {
  const r = RANGES.find((x) => x.key === key)!
  const since = new Date(now.getTime() - r.hours * 3_600_000).toISOString().replace('.000Z', 'Z')
  return { since, step: r.step }
}

export const pageCount = (total: number, size: number) => Math.max(1, Math.ceil(total / size))

const EMPTY: Totals = { requests: 0, input_tokens: 0, output_tokens: 0, cache_read: 0, cache_write: 0, cost_usd: null }

export function providerTiles(limits: Record<string, ProviderLimit>, usage: Record<string, Totals>, now: Date, locale: string) {
  return Object.entries(limits)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, p]) => {
      const totals = usage[name] ?? EMPTY
      return {
        name,
        unlimited: p.kind === 'unlimited',
        windows: WINDOWS.filter((w) => p.windows?.[w]).map((w) => {
          const win = p.windows![w]
          return { name: w, pct: win.utilization, level: level(win, now), reset: fmtReset(win.reset, now, locale) }
        }),
        totals,
        hitRate: hitRate(totals.input_tokens, totals.cache_read, totals.cache_write),
      }
    })
}

export function statsSeries(st: Stats) {
  const s = st.series
  const start = new Date(st.start).getTime()
  return {
    times: s.input_tokens.map((_, i) => new Date(start + i * st.step * 1000)),
    tokens: s.input_tokens.map((v, i) => v + s.output_tokens[i] + s.cache_read[i] + s.cache_write[i]),
    hitRate: s.input_tokens.map((v, i) => hitRate(v, s.cache_read[i], s.cache_write[i])),
  }
}
