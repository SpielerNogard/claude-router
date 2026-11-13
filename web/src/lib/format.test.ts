import { describe, expect, it } from 'vitest'
import { fmtCompact, fmtInt, fmtMs, fmtPct, fmtUSD } from './format'

describe('format', () => {
  it('shows a dash for unknown values', () => {
    expect(fmtPct(null, 'en')).toBe('–')
    expect(fmtUSD(null, 'en')).toBe('–')
  })
  it('shortens large token counts', () => {
    expect(fmtCompact(5_432_100, 'en')).toBe('5.4M')
    expect(fmtCompact(5_432_100, 'de')).toBe('5,4 Mio.')
    expect(fmtCompact(812, 'en')).toBe('812')
  })
  it('formats per locale', () => {
    expect(fmtInt(1234567, 'en')).toBe('1,234,567')
    expect(fmtInt(1234567, 'de')).toBe('1.234.567')
    expect(fmtPct(0.873, 'en')).toBe('87.3%')
    expect(fmtUSD(0.4, 'en')).toBe('$0.40')
  })
  it('switches durations to seconds from one second on', () => {
    expect(fmtMs(820, 'en')).toBe('820 ms')
    expect(fmtMs(8120, 'en')).toBe('8.1 s')
  })
})
