const DASH = '–'

export const fmtInt = (n: number, locale: string) => new Intl.NumberFormat(locale).format(n)

export const fmtCompact = (n: number, locale: string) =>
  new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(n)

export const fmtPct = (v: number | null, locale: string) =>
  v === null ? DASH : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }).format(v)

export const fmtUSD = (v: number | null, locale: string) =>
  v === null ? DASH : new Intl.NumberFormat(locale, { style: 'currency', currency: 'USD' }).format(v)

export const fmtTime = (ts: string, locale: string) =>
  new Date(ts).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit' })

export const fmtMs = (ms: number, locale: string) =>
  ms < 1000
    ? `${fmtInt(ms, locale)} ms`
    : `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(ms / 1000)} s`
