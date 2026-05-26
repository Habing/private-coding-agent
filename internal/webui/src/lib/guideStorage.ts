const WELCOME_SEEN_KEY = 'pca-guide-welcome-seen'

export function hasSeenWelcomeGuide(): boolean {
  try {
    return window.localStorage.getItem(WELCOME_SEEN_KEY) === '1'
  } catch {
    return true
  }
}

export function markWelcomeGuideSeen(): void {
  try {
    window.localStorage.setItem(WELCOME_SEEN_KEY, '1')
  } catch {
    // ignore quota / private mode
  }
}
