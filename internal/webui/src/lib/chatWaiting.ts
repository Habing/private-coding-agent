import type { AgentEvent } from '@/types/api'

/** Show typing indicator after the latest user message until assistant text or turn ends. */
export function shouldShowWaitingIndicator(
  events: AgentEvent[],
  awaitingReply: boolean,
): boolean {
  if (!awaitingReply) return false

  let lastUserIdx = -1
  for (let i = events.length - 1; i >= 0; i--) {
    if (events[i].kind === 'user') {
      lastUserIdx = i
      break
    }
  }
  if (lastUserIdx < 0) return false

  const afterUser = events.slice(lastUserIdx + 1)
  if (afterUser.some((e) => e.kind === 'error')) return false

  return !afterUser.some(
    (e) =>
      (e.kind === 'assistant_delta' || e.kind === 'assistant_message') &&
      (e.text?.length ?? 0) > 0,
  )
}
