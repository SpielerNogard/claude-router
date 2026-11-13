export const DOC_KINDS = ['in', 'translated', 'upstream', 'out', 'headers'] as const
export type DocKind = (typeof DOC_KINDS)[number]

export const docTabs = (docs: Record<string, string>): DocKind[] => DOC_KINDS.filter((k) => docs[k])

// The marker is appended by MsgLog.truncate (internal/store/msglog.go).
export const isTruncated = (body: string) => /\n… (truncated, \d+ bytes dropped|gekürzt, \d+ Bytes verworfen)$/.test(body)

export function prettyBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}
