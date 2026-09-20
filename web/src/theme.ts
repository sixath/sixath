export type ThemeMode = 'light' | 'dark'

export const THEME_STORAGE_KEY = 'sixath-theme'

export function readStoredTheme(): ThemeMode {
  try {
    const v = localStorage.getItem(THEME_STORAGE_KEY)
    if (v === 'dark' || v === 'light') return v
  } catch {
    /* ignore */
  }
  return 'light'
}

export function applyTheme(mode: ThemeMode): void {
  const root = document.documentElement
  root.setAttribute('data-theme', mode)
  root.style.colorScheme = mode
}

export function persistTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, mode)
  } catch {
    /* ignore */
  }
}

export function setTheme(mode: ThemeMode): void {
  persistTheme(mode)
  applyTheme(mode)
}

export function toggleTheme(current: ThemeMode): ThemeMode {
  const next: ThemeMode = current === 'dark' ? 'light' : 'dark'
  setTheme(next)
  return next
}

/** Call before first paint to avoid theme flash. */
export function bootstrapTheme(): ThemeMode {
  const mode = readStoredTheme()
  applyTheme(mode)
  return mode
}
