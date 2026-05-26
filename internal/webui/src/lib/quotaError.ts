/** Parse `quota exceeded: llm.tokens ... used=N cap=M` from agent/API errors. */
export function parseLLMQuotaExceeded(text: string): { used: number; cap: number } | null {
  const m = /quota exceeded:\s*llm\.tokens\b.*\bused=(\d+)\s+cap=(\d+)/i.exec(text)
  if (!m) return null
  return { used: Number(m[1]), cap: Number(m[2]) }
}

export function formatLLMQuotaExceeded(used: number, cap: number): string {
  return (
    `今日 LLM 用量已达上限（${used.toLocaleString('zh-CN')} / ${cap.toLocaleString('zh-CN')} tokens）。` +
    '配额将在 UTC 零点自动重置，或请联系管理员调高 PCA_QUOTA_LLM_TOKENS_PER_DAY。'
  )
}

/** Map raw backend quota errors to user-facing Chinese when possible. */
export function formatQuotaErrorMessage(text: string): string {
  const llm = parseLLMQuotaExceeded(text)
  if (llm) return formatLLMQuotaExceeded(llm.used, llm.cap)

  if (/quota exceeded:\s*sandbox\.active/i.test(text)) {
    return '沙箱配额已满：活跃会话数已达上限。请归档闲置会话，或联系管理员调高 sandbox_max_active。'
  }

  if (/quota exceeded:\s*tool\.invoke/i.test(text)) {
    return '工具调用过于频繁，请稍后再试。'
  }

  return text
}
