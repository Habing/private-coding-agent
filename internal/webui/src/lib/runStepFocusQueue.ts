import { useCallback, useEffect, useRef } from 'react'

/** Queue SSE step ids so each step stays visible for at least `minDisplayMs` during fast runs. */
export function useRunStepFocusQueue(
  onFocus: (stepId: string) => void,
  minDisplayMs = 400,
) {
  const queueRef = useRef<string[]>([])
  const drainingRef = useRef(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const onFocusRef = useRef(onFocus)
  onFocusRef.current = onFocus

  const drain = useCallback(() => {
    const next = queueRef.current.shift()
    if (!next) {
      drainingRef.current = false
      return
    }
    drainingRef.current = true
    onFocusRef.current(next)
    timerRef.current = setTimeout(() => {
      drain()
    }, minDisplayMs)
  }, [minDisplayMs])

  const enqueue = useCallback(
    (stepId: string) => {
      queueRef.current.push(stepId)
      if (!drainingRef.current) drain()
    },
    [drain],
  )

  const reset = useCallback(() => {
    queueRef.current = []
    drainingRef.current = false
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [])

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [])

  return { enqueue, reset }
}
