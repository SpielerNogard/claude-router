import { describe, expect, it } from 'vitest'
import { parseTheme } from './theme'

describe('parseTheme', () => {
  it('accepts known themes and falls back to auto', () => {
    expect(parseTheme('dark')).toBe('dark')
    expect(parseTheme('light')).toBe('light')
    expect(parseTheme('auto')).toBe('auto')
    expect(parseTheme(null)).toBe('auto')
    expect(parseTheme('purple')).toBe('auto')
  })
})
