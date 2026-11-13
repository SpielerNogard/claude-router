export type LimitWindow = { utilization: number; reset: string; status: string }
export type ProviderLimit = { kind: 'window' | 'unlimited'; measured_at?: string; windows?: Record<string, LimitWindow> }
export type Level = 'ok' | 'warn' | 'bad' | 'stale'

export const WINDOWS = ['5h', '7d'] as const

// After the reset the last value is stale until a new response refreshes it.
export function level(w: LimitWindow, now: Date): Level {
  if (new Date(w.reset).getTime() <= now.getTime()) return 'stale'
  if (w.status === 'rejected' || w.utilization >= 1) return 'bad'
  return w.utilization >= 0.8 ? 'warn' : 'ok'
}

export function fmtReset(ts: string, now: Date, locale: string): string {
  const d = new Date(ts)
  const time = d.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
  if (d.toDateString() === now.toDateString()) return time
  return `${d.toLocaleDateString(locale, { weekday: 'short' })} ${time}`
}
