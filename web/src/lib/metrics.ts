import type { Series } from './api'

export function hitRate(input: number, cacheRead: number, cacheWrite: number): number | null {
  const total = input + cacheRead + cacheWrite
  return total === 0 ? null : cacheRead / total
}

export type Totals = {
  requests: number; tokensIn: number; tokensOut: number; cacheRead: number; cacheWrite: number
  hitRate: number | null; costUSD: number
}

const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0)

export function seriesTotals(s: Series): Totals {
  const tokensIn = sum(s.tokens_in), cacheRead = sum(s.cache_read), cacheWrite = sum(s.cache_write)
  return {
    requests: sum(s.requests), tokensIn, tokensOut: sum(s.tokens_out), cacheRead, cacheWrite,
    hitRate: hitRate(tokensIn, cacheRead, cacheWrite), costUSD: sum(s.cost_usd),
  }
}

export const seriesHitRates = (s: Series) => s.tokens_in.map((v, i) => hitRate(v, s.cache_read[i], s.cache_write[i]))

export const seriesTokens = (s: Series) =>
  s.tokens_in.map((v, i) => v + s.tokens_out[i] + s.cache_read[i] + s.cache_write[i])
