const STORAGE_KEY = 'pca.ui.sessionSidebarOpen'

export function readSessionSidebarOpen(): boolean {
  try {
    const v = window.localStorage.getItem(STORAGE_KEY)
    if (v === '0' || v === 'false') return false
    if (v === '1' || v === 'true') return true
  } catch {
    // ignore
  }
  return true
}

export function writeSessionSidebarOpen(open: boolean): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, open ? '1' : '0')
  } catch {
    // ignore
  }
}
