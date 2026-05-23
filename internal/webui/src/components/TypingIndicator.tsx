import { cn } from '@/lib/utils'

export interface TypingIndicatorProps {
  label?: string
  className?: string
}

/** Assistant-side placeholder while the agent has not started text output yet. */
export function TypingIndicator({ label, className }: TypingIndicatorProps) {
  return (
    <div className={cn('flex justify-start', className)}>
      <div
        role="status"
        aria-live="polite"
        aria-label={label ?? '正在等待回复'}
        className="flex max-w-[80%] items-center gap-2 rounded-2xl border bg-card px-4 py-3 text-sm shadow-sm"
      >
        <span className="flex items-center gap-1" aria-hidden="true">
          {[0, 1, 2].map((i) => (
            <span
              key={i}
              className="h-2 w-2 rounded-full bg-muted-foreground/70 motion-safe:animate-bounce"
              style={{ animationDelay: `${i * 150}ms`, animationDuration: '0.9s' }}
            />
          ))}
        </span>
        {label && (
          <span className="text-xs text-muted-foreground">{label}</span>
        )}
      </div>
    </div>
  )
}
