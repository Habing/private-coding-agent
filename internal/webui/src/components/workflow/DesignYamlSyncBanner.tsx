import { Button } from '@/components/ui/button'

export interface DesignYamlSyncBannerProps {
  mode: 'expert' | 'designer'
  onSyncToDesigner: () => void
  onDismiss?: () => void
}

export function DesignYamlSyncBanner({
  mode,
  onSyncToDesigner,
  onDismiss,
}: DesignYamlSyncBannerProps) {
  return (
    <div
      className="flex flex-col gap-2 rounded-md border border-amber-500/40 bg-amber-50 px-3 py-2 text-sm dark:bg-amber-950/30"
      role="status"
    >
      <p className="text-amber-900 dark:text-amber-100">
        {mode === 'expert'
          ? '当前 YAML 与可视化设计器不一致。保存后请先同步，否则设计器仍显示旧模型。'
          : '专家模式中的 YAML 已修改，设计器尚未刷新。'}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button type="button" size="sm" variant="default" onClick={onSyncToDesigner}>
          同步到设计器
        </Button>
        {onDismiss && (
          <Button type="button" size="sm" variant="ghost" onClick={onDismiss}>
            稍后
          </Button>
        )}
      </div>
    </div>
  )
}
