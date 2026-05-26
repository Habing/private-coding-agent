import { CircleHelp, X } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import {
  FEATURE_GUIDES,
  QUICK_START_STEPS,
  featureGuidesForUser,
} from '@/lib/featureGuide'
import { isAdmin } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'

export interface FeatureGuidePanelProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const AREA_LABELS = {
  sidebar: '左侧栏',
  main: '主区域',
  topbar: '顶部导航',
} as const

export function FeatureGuidePanel({ open, onOpenChange }: FeatureGuidePanelProps) {
  const user = useAuthStore((s) => s.user)
  const admin = isAdmin(user)
  const guides = featureGuidesForUser(admin)

  if (!open) return null

  function close() {
    onOpenChange(false)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 sm:items-center"
      role="presentation"
      onClick={close}
    >
      <Card
        role="dialog"
        aria-modal="true"
        aria-labelledby="feature-guide-title"
        className="my-4 w-full max-w-2xl shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <CardHeader className="flex flex-row items-start justify-between gap-3 space-y-0 pb-3">
          <div className="space-y-1">
            <CardTitle id="feature-guide-title" className="flex items-center gap-2 text-lg">
              <CircleHelp className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
              使用指引
            </CardTitle>
            <CardDescription>
              快速了解各功能用途，按你的角色只展示可用入口。
            </CardDescription>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-8 w-8 shrink-0"
            aria-label="关闭"
            onClick={close}
          >
            <X className="h-4 w-4" />
          </Button>
        </CardHeader>
        <CardContent className="max-h-[min(70vh,640px)] space-y-5 overflow-y-auto pt-0">
          <section aria-labelledby="quick-start-heading">
            <h3 id="quick-start-heading" className="mb-2 text-sm font-semibold">
              快速上手
            </h3>
            <ol className="space-y-3">
              {QUICK_START_STEPS.map((step, i) => (
                <li key={step.title} className="flex gap-3 text-sm">
                  <span
                    className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-medium text-primary-foreground"
                    aria-hidden="true"
                  >
                    {i + 1}
                  </span>
                  <div>
                    <div className="font-medium">{step.title}</div>
                    <p className="mt-0.5 text-muted-foreground">{step.description}</p>
                  </div>
                </li>
              ))}
            </ol>
          </section>

          <Separator />

          <section aria-labelledby="features-heading">
            <h3 id="features-heading" className="mb-2 text-sm font-semibold">
              功能说明
            </h3>
            <ul className="space-y-2">
              {guides.map((item) => (
                <li
                  key={item.id}
                  className="rounded-md border bg-muted/30 px-3 py-2.5 text-sm"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{item.title}</span>
                    <span
                      className={cn(
                        'rounded-full px-2 py-0.5 text-[10px] font-medium',
                        item.area === 'topbar'
                          ? 'bg-secondary text-secondary-foreground'
                          : 'bg-muted text-muted-foreground',
                      )}
                    >
                      {AREA_LABELS[item.area]}
                    </span>
                    {item.adminOnly && (
                      <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-medium text-amber-900">
                        管理员
                      </span>
                    )}
                  </div>
                  <p className="mt-1 text-muted-foreground">{item.description}</p>
                  {item.to && (
                    <Link
                      to={item.to}
                      className="mt-1.5 inline-block text-xs text-primary underline-offset-2 hover:underline"
                      onClick={close}
                    >
                      前往 {item.title} →
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </section>

          {!admin && FEATURE_GUIDES.some((f) => f.adminOnly) && (
            <p className="text-xs text-muted-foreground">
              部分管理功能（工作流、MCP、审计等）仅管理员可见；如需使用请联系租户管理员。
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
