import { describe, expect, it } from 'vitest'
import { fmtReset, level } from './limits'

const now = new Date('2026-10-02T10:00:00Z')
const win = (utilization: number, reset: string, status = 'allowed') => ({ utilization, reset, status })

describe('level', () => {
  it('stays calm below 80 %', () => {
    expect(level(win(0.64, '2026-10-02T15:00:00Z'), now)).toBe('ok')
  })
  it('warns from 80 %', () => {
    expect(level(win(0.8, '2026-10-02T15:00:00Z'), now)).toBe('warn')
  })
  it('is bad when rejected or exhausted', () => {
    expect(level(win(0.5, '2026-10-02T15:00:00Z', 'rejected'), now)).toBe('bad')
    expect(level(win(1, '2026-10-02T15:00:00Z'), now)).toBe('bad')
  })
  it('is stale once the window has reset', () => {
    expect(level(win(1, '2026-10-02T09:59:00Z', 'rejected'), now)).toBe('stale')
  })
})

describe('fmtReset', () => {
  it('shows only the time for today', () => {
    expect(fmtReset('2026-10-02T15:00:00Z', now, 'en')).toBe('03:00 PM')
  })
  it('adds the weekday for a later day', () => {
    expect(fmtReset('2026-10-04T18:00:00Z', now, 'en')).toBe('Sun 06:00 PM')
    expect(fmtReset('2026-10-04T18:00:00Z', now, 'de')).toBe('So 18:00')
  })
})
