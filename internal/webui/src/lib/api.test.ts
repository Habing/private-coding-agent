import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError, api } from './api'

const realFetch = global.fetch

function mockFetchOnce(init: { status: number; body?: unknown; ctype?: string }) {
  const headers = new Headers()
  if (init.ctype) headers.set('Content-Type', init.ctype)
  const body =
    init.body === undefined || init.status === 204
      ? null
      : typeof init.body === 'string'
        ? init.body
        : JSON.stringify(init.body)
  const resp = new Response(body, { status: init.status, headers })
  const spy = vi.fn(async () => resp)
  global.fetch = spy as unknown as typeof fetch
  return spy
}

describe('api', () => {
  beforeEach(() => {
    global.fetch = realFetch
  })
  afterEach(() => {
    vi.restoreAllMocks()
    global.fetch = realFetch
  })

  it('returns parsed JSON on 200', async () => {
    mockFetchOnce({ status: 200, body: { ok: true }, ctype: 'application/json' })
    const out = await api<{ ok: boolean }>('/x')
    expect(out).toEqual({ ok: true })
  })

  it('returns undefined on 204', async () => {
    mockFetchOnce({ status: 204 })
    const out = await api<unknown>('/x', { method: 'DELETE' })
    expect(out).toBeUndefined()
  })

  it('sets Authorization when token provided', async () => {
    const spy = mockFetchOnce({ status: 200, body: {}, ctype: 'application/json' })
    await api('/x', { token: 'jwt.demo' })
    const init = (spy.mock.calls[0] as unknown as [string, RequestInit])[1]
    const headers = new Headers(init.headers)
    expect(headers.get('Authorization')).toBe('Bearer jwt.demo')
  })

  it('sets Content-Type when body present and not already set', async () => {
    const spy = mockFetchOnce({ status: 200, body: {}, ctype: 'application/json' })
    await api('/x', { method: 'POST', body: JSON.stringify({ a: 1 }) })
    const init = (spy.mock.calls[0] as unknown as [string, RequestInit])[1]
    const headers = new Headers(init.headers)
    expect(headers.get('Content-Type')).toBe('application/json')
  })

  it('throws ApiError with status + body on non-2xx', async () => {
    mockFetchOnce({ status: 401, body: 'unauthorized', ctype: 'text/plain' })
    await expect(api('/x')).rejects.toMatchObject({
      name: 'ApiError',
      status: 401,
      body: 'unauthorized',
    })
  })

  it('ApiError is an Error subclass', async () => {
    mockFetchOnce({ status: 500, body: 'boom', ctype: 'text/plain' })
    try {
      await api('/x')
      throw new Error('should have thrown')
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError)
      expect(err).toBeInstanceOf(Error)
    }
  })

  it('treats non-JSON 2xx as undefined', async () => {
    mockFetchOnce({ status: 200, body: '<html></html>', ctype: 'text/html' })
    const out = await api('/x')
    expect(out).toBeUndefined()
  })
})
