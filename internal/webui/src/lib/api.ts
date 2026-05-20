export class ApiError extends Error {
  constructor(
    public status: number,
    public body: string,
  ) {
    super(body || `HTTP ${status}`)
    this.name = 'ApiError'
  }
}

export interface ApiOptions extends RequestInit {
  token?: string | null
}

export async function api<T>(path: string, opts: ApiOptions = {}): Promise<T> {
  const { token, headers: rawHeaders, ...rest } = opts
  const headers = new Headers(rawHeaders)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  if (rest.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const resp = await fetch(path, { ...rest, headers })
  if (!resp.ok) {
    const body = await resp.text().catch(() => '')
    throw new ApiError(resp.status, body)
  }
  if (resp.status === 204) return undefined as T
  const ctype = resp.headers.get('Content-Type') ?? ''
  if (!ctype.includes('application/json')) return undefined as T
  return (await resp.json()) as T
}
