import { describe, expect, it } from 'vitest'
import { nextDelay } from './poll'

describe('nextDelay', () => {
  it('uses the base interval while healthy', () => {
    expect(nextDelay(2000, 0)).toBe(2000)
  })
  it('doubles per failure up to 30 s', () => {
    expect(nextDelay(2000, 1)).toBe(4000)
    expect(nextDelay(2000, 3)).toBe(16000)
    expect(nextDelay(2000, 10)).toBe(30000)
  })
})
