import { ref } from 'vue'

export type RequestRow = {
  id: number
  ts: string
  alias: string
  provider: string
  model: string
  auth_mode: string
  input_tokens: number
  output_tokens: number
  cache_read: number
  cache_write: number
  cost_usd: number | null
  duration_ms: number
  status: 'ok' | 'partial' | 'error'
  error_code: string
  client: string
  request_id: string
}

export type Series = {
  key: string
  billed: boolean
  cost_usd: number[]
  tokens_in: number[]
  tokens_out: number[]
  cache_read: number[]
  cache_write: number[]
  requests: number[]
}

export type Usage = { buckets: string[]; series: Series[] }

export type Totals = {
  requests: number
  input_tokens: number
  output_tokens: number
  cache_read: number
  cache_write: number
  cost_usd: number | null
}

export type Stats = {
  totals: Totals
  by_provider: Record<string, Totals>
  start: string
  step: number
  series: { input_tokens: number[]; output_tokens: number[]; cache_read: number[]; cache_write: number[] }
}
export type Health = { status: string; blocked?: Record<string, string> }

export class AuthError extends Error {}

export const authNeeded = ref(false)
export const connected = ref(true)

const TOKEN_KEY = 'router-token'

// localStorage can throw in private mode or with blocked site data.
export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

export function setToken(t: string | null): void {
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* token then only lasts until reload */
  }
}

export async function getJSON<T>(path: string, fetchFn: typeof fetch = fetch): Promise<T> {
  const token = getToken()
  let res: Response
  try {
    res = await fetchFn(path, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
  } catch (e) {
    connected.value = false
    throw e
  }
  if (res.status === 401) {
    setToken(null)
    authNeeded.value = true
    connected.value = false
    throw new AuthError('unauthorized')
  }
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error ?? `HTTP ${res.status}`)
  connected.value = true
  return body as T
}

