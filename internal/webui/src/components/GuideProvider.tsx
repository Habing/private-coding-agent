import { useMemo, useState, type ReactNode } from 'react'

import { FeatureGuidePanel } from '@/components/FeatureGuidePanel'
import { GuideContext } from '@/components/guideContext'

export function GuideProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const value = useMemo(() => ({ openGuide: () => setOpen(true) }), [])

  return (
    <GuideContext.Provider value={value}>
      {children}
      <FeatureGuidePanel open={open} onOpenChange={setOpen} />
    </GuideContext.Provider>
  )
}
