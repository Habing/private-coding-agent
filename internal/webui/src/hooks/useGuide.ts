import { useContext } from 'react'

import { GuideContext } from '@/components/guideContext'

export function useGuide() {
  const ctx = useContext(GuideContext)
  if (!ctx) {
    throw new Error('useGuide must be used within GuideProvider')
  }
  return ctx
}
