import { describe, expect, it } from 'vitest'
import { docTabs, isTruncated, prettyBody } from './docs'

describe('docTabs', () => {
  it('keeps the fixed order and drops missing or empty docs', () => {
    expect(docTabs({ out: 'x', in: 'y', headers: '', upstream: 'z' })).toEqual(['in', 'upstream', 'out'])
    expect(docTabs({})).toEqual([])
  })
})

describe('prettyBody', () => {
  it('indents JSON', () => {
    expect(prettyBody('{"a":1}')).toBe('{\n  "a": 1\n}')
  })
  it('leaves SSE streams and broken JSON untouched', () => {
    const sse = 'event: message_start\ndata: {"type":"message_start"}\n\n'
    expect(prettyBody(sse)).toBe(sse)
    expect(prettyBody('{"a":')).toBe('{"a":')
  })
})

describe('isTruncated', () => {
  it('detects the marker the router appends at max_body_kb', () => {
    const cut = '{"x":"aaaa\n… truncated, 7000 bytes dropped'
    expect(isTruncated(cut)).toBe(true)
    expect(prettyBody(cut)).toBe(cut)
    expect(isTruncated('{"x":1}')).toBe(false)
  })
  it('ignores the marker text when it appears inside a body instead of at its end', () => {
    expect(isTruncated('{"text":"\n… truncated, 12 bytes dropped","more":1}')).toBe(false)
  })
})
