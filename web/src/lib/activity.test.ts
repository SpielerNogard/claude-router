import { describe, expect, it } from 'vitest'
import type { Series, Usage } from './api'
import { bucketTotals, dailyPoints, weekdayHourGrid } from './activity'

const series = (key: string, requests: number[], cost: number[] = requests.map(() => 0)): Series => ({
  key, billed: true, requests, cost_usd: cost,
  tokens_in: requests.map((r) => r * 10), tokens_out: requests.map(() => 1),
  cache_read: requests.map(() => 0), cache_write: requests.map(() => 0),
})

describe('bucketTotals', () => {
  it('sums all series per bucket for the chosen metric', () => {
    const u: Usage = { buckets: ['2026-10-01', '2026-10-02'], series: [series('a', [1, 2], [0.5, 0]), series('b', [3, 0])] }
    expect(bucketTotals(u, 'requests')).toEqual([4, 2])
    expect(bucketTotals(u, 'tokens')).toEqual([42, 22])
    expect(bucketTotals(u, 'cost')).toEqual([0.5, 0])
    expect(dailyPoints(u, 'requests')).toEqual([['2026-10-01', 4], ['2026-10-02', 2]])
  })
  it('handles no series', () => {
    expect(bucketTotals({ buckets: ['2026-10-01'], series: [] }, 'requests')).toEqual([0])
  })
})

describe('weekdayHourGrid', () => {
  it('always returns 7x24 cells', () => {
    expect(weekdayHourGrid({ buckets: [], series: [] }, 'requests', 'UTC')).toHaveLength(168)
  })
  it('converts UTC hour buckets to local weekday and hour, across DST', () => {
    // Mon 2026-03-23 22 UTC = Mon 23:00 Berlin (CET), Mon 2026-03-30 22 UTC = Tue 00:00 (CEST)
    const u: Usage = { buckets: ['2026-03-23T22', '2026-03-30T22'], series: [series('a', [5, 7])] }
    const grid = weekdayHourGrid(u, 'requests', 'Europe/Berlin')
    const at = (h: number, d: number) => grid.find(([gh, gd]) => gh === h && gd === d)![2]
    expect(at(23, 0)).toBe(5)
    expect(at(0, 1)).toBe(7)
  })
})
