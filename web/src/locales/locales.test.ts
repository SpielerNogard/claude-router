import { describe, expect, it } from 'vitest'
import en from './en.json'
import de from './de.json'

function keys(obj: object, prefix = ''): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    typeof v === 'object' && v !== null ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  )
}

describe('locales', () => {
  it('de has exactly the keys of en', () => {
    expect(keys(de).sort()).toEqual(keys(en).sort())
  })
})
