import { describe, expect, it } from 'vitest'
import type { Series } from './api'
import { hitRate, seriesHitRates, seriesTokens, seriesTotals } from './metrics'

describe('hitRate', () => {
  it('is null without any input tokens', () => {
    expect(hitRate(0, 0, 0)).toBeNull()
  })
  it('counts cache reads against all input', () => {
    expect(hitRate(10, 80, 10)).toBeCloseTo(0.8)
  })
})

describe('series', () => {
  const s: Series = {
    key: 'claude-opus-5-5', billed: false, cost_usd: [1, 2], tokens_in: [10, 0], tokens_out: [5, 0],
    cache_read: [90, 0], cache_write: [0, 0], requests: [3, 0],
  }
  it('totals and hit rates', () => {
    const t = seriesTotals(s)
    expect(t.requests).toBe(3)
    expect(t.hitRate).toBeCloseTo(0.9)
    expect(t.costUSD).toBe(3)
    expect(seriesHitRates(s)).toEqual([0.9, null])
    expect(seriesTokens(s)).toEqual([105, 0])
  })
})
