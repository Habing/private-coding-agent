import ReactMarkdown from 'react-markdown'
import type { Components } from 'react-markdown'

import { cn } from '@/lib/utils'

const markdownComponents: Components = {
  p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
  ul: ({ children }) => (
    <ul className="mb-2 list-disc space-y-1 pl-5 last:mb-0">{children}</ul>
  ),
  ol: ({ children }) => (
    <ol className="mb-2 list-decimal space-y-1 pl-5 last:mb-0">{children}</ol>
  ),
  li: ({ children }) => <li className="leading-relaxed">{children}</li>,
  h1: ({ children }) => (
    <h1 className="mb-2 text-base font-semibold last:mb-0">{children}</h1>
  ),
  h2: ({ children }) => (
    <h2 className="mb-2 text-sm font-semibold last:mb-0">{children}</h2>
  ),
  h3: ({ children }) => (
    <h3 className="mb-1 text-sm font-medium last:mb-0">{children}</h3>
  ),
  blockquote: ({ children }) => (
    <blockquote className="mb-2 border-l-2 border-border pl-3 text-muted-foreground last:mb-0">
      {children}
    </blockquote>
  ),
  code: ({ className, children, ...props }) => {
    const inline = !className
    if (inline) {
      return (
        <code
          className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]"
          {...props}
        >
          {children}
        </code>
      )
    }
    return (
      <code
        className={cn('font-mono text-[0.85em]', className)}
        {...props}
      >
        {children}
      </code>
    )
  },
  pre: ({ children }) => (
    <pre className="mb-2 overflow-x-auto rounded-md bg-muted p-2 font-mono text-xs last:mb-0">
      {children}
    </pre>
  ),
  a: ({ href, children }) => (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-primary underline underline-offset-2"
    >
      {children}
    </a>
  ),
  strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
  em: ({ children }) => <em className="italic">{children}</em>,
  hr: () => <hr className="my-2 border-border" />,
}

export interface MarkdownContentProps {
  source: string
  className?: string
}

export function MarkdownContent({ source, className }: MarkdownContentProps) {
  if (!source) return null
  return (
    <div className={cn('markdown-content break-words', className)}>
      <ReactMarkdown components={markdownComponents}>{source}</ReactMarkdown>
    </div>
  )
}
