import { Input } from '@/components/ui/input'
import { ExprPicker } from '@/components/workflow/ExprPicker'
import type { WorkflowDesign } from '@/types/api'

export interface ExprFieldProps {
  design: WorkflowDesign
  currentStepId?: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  className?: string
}

export function ExprField({
  design,
  currentStepId,
  value,
  onChange,
  placeholder,
  className,
}: ExprFieldProps) {
  const isExpr = value.includes('${')

  return (
    <div className={`flex flex-wrap items-center gap-1 ${className ?? ''}`}>
      <Input
        className={`min-w-0 flex-1 font-mono text-xs ${isExpr ? 'border-primary/50' : ''}`}
        value={value}
        placeholder={placeholder ?? '值或 ${expr}'}
        onChange={(e) => onChange(e.target.value)}
      />
      <ExprPicker
        design={design}
        currentStepId={currentStepId}
        onSelect={onChange}
      />
    </div>
  )
}
