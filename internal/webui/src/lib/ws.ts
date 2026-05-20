export function wsURL(sessionId: string, token: string): string {
  const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  return `${scheme}//${host}/sessions/${encodeURIComponent(
    sessionId,
  )}/ws?token=${encodeURIComponent(token)}`
}
