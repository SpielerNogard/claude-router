import { describe, expect, it } from 'vitest'
import type { Stats } from './api'
import { pageCount, providerTiles, rangeQuery, statsSeries } from './live'

const now = new Date('2026-10-02T10:00:00Z')
const totals = (requests: number, input = 0, cache_read = 0, cost_usd: number | null = null) =>
  ({ requests, input_tokens: input, output_tokens: 0, cache_read, cache_write: 0, cost_usd })

describe('rangeQuery', () => {
  it('maps each range to since and a step that keeps the chart readable', () => {
    expect(rangeQuery('1h', now)).toEqual({ since: '2026-10-02T09:00:00Z', step: 60 })
    expect(rangeQuery('24h', now)).toEqual({ since: '2026-10-01T10:00:00Z', step: 1800 })
    expect(rangeQuery('7d', now).step).toBe(10800)
  })
})

describe('pageCount', () => {
  it('has at least one page and rounds up', () => {
    expect(pageCount(0, 50)).toBe(1)
    expect(pageCount(50, 50)).toBe(1)
    expect(pageCount(51, 50)).toBe(2)
  })
})

describe('providerTiles', () => {
  it('combines limits and usage per configured provider, sorted by name', () => {
    const tiles = providerTiles(
      {
        lmstudio: { kind: 'unlimited' },
        account1: { kind: 'window', windows: { '7d': { utilization: 0.87, reset: '2026-10-04T18:00:00Z', status: 'allowed' } } },
        account2: { kind: 'window' },
      },
      { account1: totals(3, 10, 90, 0.4) },
      now,
      'en',
    )
    expect(tiles.map((t) => t.name)).toEqual(['account1', 'account2', 'lmstudio'])
    expect(tiles[0].windows).toEqual([{ name: '7d', pct: 0.87, level: 'warn', reset: 'Sun 06:00 PM' }])
    expect(tiles[0].totals.requests).toBe(3)
    expect(tiles[0].hitRate).toBeCloseTo(0.9)
    expect(tiles[1].windows).toEqual([])
    expect(tiles[1].totals.requests).toBe(0)
    expect(tiles[1].hitRate).toBeNull()
    expect(tiles[2].unlimited).toBe(true)
  })
})

describe('statsSeries', () => {
  it('derives bucket times, total tokens and hit rate', () => {
    const st: Stats = {
      totals: totals(0), by_provider: {}, start: '2026-10-02T09:58:00Z', step: 60,
      series: { input_tokens: [10, 0], output_tokens: [5, 0], cache_read: [30, 0], cache_write: [0, 0] },
    }
    const s = statsSeries(st)
    expect(s.times.map((d) => d.toISOString())).toEqual(['2026-10-02T09:58:00.000Z', '2026-10-02T09:59:00.000Z'])
    expect(s.tokens).toEqual([45, 0])
    expect(s.hitRate).toEqual([0.75, null])
  })
})
