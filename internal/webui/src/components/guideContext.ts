import { createContext } from 'react'

export interface GuideContextValue {
  openGuide: () => void
}

export const GuideContext = createContext<GuideContextValue | null>(null)
