import { Send } from 'lucide-react'
import { useState, type KeyboardEvent } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export interface ComposerProps {
  onSend: (content: string) => void | Promise<void>
  disabled: boolean
  placeholder?: string
}

export function Composer({ onSend, disabled, placeholder = '输入消息…' }: ComposerProps) {
  const [value, setValue] = useState('')
  const [pending, setPending] = useState(false)

  async function submit() {
    const trimmed = value.trim()
    if (!trimmed || disabled || pending) return
    setPending(true)
    setValue('')
    try {
      await onSend(trimmed)
    } finally {
      setPending(false)
    }
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      void submit()
    }
  }

  const sendDisabled = disabled || pending || value.trim().length === 0

  return (
    <div className="flex items-end gap-2 border-t bg-background p-3">
      <textarea
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={onKeyDown}
        disabled={disabled || pending}
        placeholder={placeholder}
        rows={2}
        className={cn(
          'flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm',
          'focus:outline-none focus:ring-1 focus:ring-ring',
          'disabled:cursor-not-allowed disabled:opacity-50',
        )}
      />
      <Button
        type="button"
        size="sm"
        aria-label="发送"
        onClick={() => void submit()}
        disabled={sendDisabled}
      >
        <Send className="mr-1 h-3.5 w-3.5" />
        发送
      </Button>
    </div>
  )
}
