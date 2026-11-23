import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthError, authNeeded, connected, getJSON, getToken, putJSON, setToken } from './api'

function memoryStorage() {
  const m = new Map<string, string>()
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  }
}

const respond = (status: number, body: unknown) =>
  vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status }))

describe('getJSON', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', memoryStorage())
    authNeeded.value = false
    connected.value = true
  })

  it('sends the stored token as bearer', async () => {
    setToken('s3cret')
    const f = respond(200, { ok: 1 })
    await getJSON('/api/usage', f)
    expect(f.mock.calls[0][1].headers).toEqual({ Authorization: 'Bearer s3cret' })
  })

  it('on 401 drops the token and asks for a new one', async () => {
    setToken('wrong')
    await expect(getJSON('/api/usage', respond(401, { error: 'x' }))).rejects.toBeInstanceOf(AuthError)
    expect(getToken()).toBeNull()
    expect(authNeeded.value).toBe(true)
    expect(connected.value).toBe(false)
  })

  it('surfaces the router error message', async () => {
    const f = respond(400, { error: 'the range yields 2161 buckets' })
    await expect(getJSON('/api/usage', f)).rejects.toThrow('the range yields 2161 buckets')
  })

  it('marks the connection down on network errors', async () => {
    await expect(getJSON('/healthz', vi.fn().mockRejectedValue(new TypeError('fetch failed')))).rejects.toThrow()
    expect(connected.value).toBe(false)
  })
})


describe('putJSON', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', memoryStorage())
    authNeeded.value = false
    connected.value = true
  })

  it('sends PUT with JSON body and bearer token', async () => {
    setToken('s3cret')
    const f = respond(200, { active: 'konto2', accounts: ['abo', 'konto2'] })
    expect(await putJSON('/api/active', { provider: 'konto2' }, f)).toEqual({ active: 'konto2', accounts: ['abo', 'konto2'] })
    const init = f.mock.calls[0][1]
    expect(init.method).toBe('PUT')
    expect(init.body).toBe('{"provider":"konto2"}')
    expect(init.headers).toEqual({ Authorization: 'Bearer s3cret', 'Content-Type': 'application/json' })
  })

  it('throws AuthError on 401', async () => {
    await expect(putJSON('/api/active', {}, respond(401, { error: 'x' }))).rejects.toBeInstanceOf(AuthError)
    expect(authNeeded.value).toBe(true)
  })

  it('throws the router error on 400', async () => {
    await expect(putJSON('/api/active', { provider: 'x' }, respond(400, { error: 'unknown account "x"' }))).rejects.toThrow(
      'unknown account "x"',
    )
  })
})
