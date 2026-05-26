import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { createPortal } from 'react-dom'

import { Button } from '@/components/ui/button'
import { buildExprTree, wrapExpr, type ExprTreeNode } from '@/lib/exprPickerTree'
import type { WorkflowDesign } from '@/types/api'

const PANEL_WIDTH = 288
const PANEL_MAX_HEIGHT = 288
const VIEWPORT_PAD = 8

function ExprTreeBranch({
  node,
  depth,
  onPick,
}: {
  node: ExprTreeNode
  depth: number
  onPick: (expr: string) => void
}) {
  const [open, setOpen] = useState(depth < 2)
  const hasChildren = (node.children?.length ?? 0) > 0

  if (!hasChildren && node.path) {
    return (
      <button
        type="button"
        className="block w-full rounded px-2 py-1 text-left text-xs hover:bg-muted"
        style={{ paddingLeft: 8 + depth * 12 }}
        title={node.hint ? `${node.hint} → ${wrapExpr(node.path!)}` : wrapExpr(node.path!)}
        onClick={() => onPick(wrapExpr(node.path!))}
      >
        <span>{node.label}</span>
        {node.hint ? (
          <span className="mt-0.5 block font-mono text-[10px] text-muted-foreground">
            {node.hint}
          </span>
        ) : null}
      </button>
    )
  }

  return (
    <div>
      <button
        type="button"
        className="flex w-full items-center gap-1 rounded px-2 py-1 text-left text-xs font-medium hover:bg-muted"
        style={{ paddingLeft: 8 + depth * 12 }}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="text-muted-foreground">{open ? '▾' : '▸'}</span>
        {node.label}
      </button>
      {open &&
        node.children?.map((c) => (
          <ExprTreeBranch key={c.id} node={c} depth={depth + 1} onPick={onPick} />
        ))}
    </div>
  )
}

function computePanelStyle(anchor: HTMLElement): CSSProperties {
  const rect = anchor.getBoundingClientRect()
  const gap = 4
  const vw = window.innerWidth
  const vh = window.innerHeight

  let left = rect.right - PANEL_WIDTH
  if (left < VIEWPORT_PAD) left = VIEWPORT_PAD
  if (left + PANEL_WIDTH > vw - VIEWPORT_PAD) {
    left = vw - PANEL_WIDTH - VIEWPORT_PAD
  }

  const spaceBelow = vh - rect.bottom - gap - VIEWPORT_PAD
  const spaceAbove = rect.top - gap - VIEWPORT_PAD
  const openBelow = spaceBelow >= 120 || spaceBelow >= spaceAbove

  let top: number
  let maxHeight: number
  if (openBelow) {
    top = rect.bottom + gap
    maxHeight = Math.min(PANEL_MAX_HEIGHT, Math.max(120, spaceBelow))
  } else {
    maxHeight = Math.min(PANEL_MAX_HEIGHT, Math.max(120, spaceAbove))
    top = Math.max(VIEWPORT_PAD, rect.top - gap - maxHeight)
    maxHeight = Math.min(maxHeight, rect.top - gap - top)
  }

  return {
    position: 'fixed',
    top,
    left,
    width: PANEL_WIDTH,
    maxHeight,
    zIndex: 9999,
  }
}

export interface ExprPickerProps {
  design: WorkflowDesign
  currentStepId?: string
  onSelect: (expr: string) => void
  triggerLabel?: string
}

export function ExprPicker({
  design,
  currentStepId,
  onSelect,
  triggerLabel = '插入变量',
}: ExprPickerProps) {
  const [open, setOpen] = useState(false)
  const [panelStyle, setPanelStyle] = useState<CSSProperties>({})
  const anchorRef = useRef<HTMLDivElement>(null)
  const tree = useMemo(
    () => buildExprTree(design, currentStepId),
    [design, currentStepId],
  )

  const reposition = useCallback(() => {
    if (!anchorRef.current) return
    setPanelStyle(computePanelStyle(anchorRef.current))
  }, [])

  useLayoutEffect(() => {
    if (!open) return
    reposition()
  }, [open, reposition, tree])

  useEffect(() => {
    if (!open) return
    const onScrollOrResize = () => reposition()
    window.addEventListener('resize', onScrollOrResize)
    window.addEventListener('scroll', onScrollOrResize, true)
    return () => {
      window.removeEventListener('resize', onScrollOrResize)
      window.removeEventListener('scroll', onScrollOrResize, true)
    }
  }, [open, reposition])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open])

  function pick(expr: string) {
    onSelect(expr)
    setOpen(false)
  }

  if (tree.length === 0) {
    return (
      <span className="text-xs text-muted-foreground">无可引用变量</span>
    )
  }

  const portal =
    open && typeof document !== 'undefined'
      ? createPortal(
          <>
            <div
              className="fixed inset-0 z-[9998]"
              aria-hidden
              onClick={() => setOpen(false)}
            />
            <div
              role="listbox"
              className="overflow-y-auto rounded-md border bg-background p-2 shadow-lg"
              style={panelStyle}
            >
              {tree.map((n) => (
                <ExprTreeBranch key={n.id} node={n} depth={0} onPick={pick} />
              ))}
            </div>
          </>,
          document.body,
        )
      : null

  return (
    <div ref={anchorRef} className="relative inline-block shrink-0">
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-7 text-xs"
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => setOpen((o) => !o)}
      >
        {triggerLabel}
      </Button>
      {portal}
    </div>
  )
}
