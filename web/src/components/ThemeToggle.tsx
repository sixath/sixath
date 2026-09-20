import { useState } from 'react'
import { readStoredTheme, toggleTheme, type ThemeMode } from '../theme'

function MoonIcon() {
  return (
    <svg className="theme-toggle__icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill="currentColor"
        d="M13.4 1.9a1.15 1.15 0 0 1 1.15 1.55 6.2 6.2 0 1 0 5.6 5.6 1.15 1.15 0 0 1 1.55 1.15A10.55 10.55 0 1 1 13.4 1.9Z"
      />
    </svg>
  )
}

function SunIcon() {
  return (
    <svg className="theme-toggle__icon" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="4.25" fill="currentColor" />
      <g stroke="currentColor" strokeWidth="1.75" strokeLinecap="round">
        <path d="M12 2.5v2.2M12 19.3v2.2M2.5 12h2.2M19.3 12h2.2" />
        <path d="M5.2 5.2l1.55 1.55M17.25 17.25l1.55 1.55M18.8 5.2l-1.55 1.55M6.75 17.25l-1.55 1.55" />
      </g>
    </svg>
  )
}

export default function ThemeToggle({ className = '' }: { className?: string }) {
  const [theme, setThemeMode] = useState<ThemeMode>(() => readStoredTheme())
  const isDark = theme === 'dark'

  return (
    <button
      type="button"
      className={`theme-toggle ${isDark ? 'is-dark' : 'is-light'} ${className}`.trim()}
      onClick={() => setThemeMode((prev) => toggleTheme(prev))}
      title={isDark ? '切换到白天模式' : '切换到黑夜模式'}
      aria-label={isDark ? '切换到白天模式' : '切换到黑夜模式'}
    >
      {isDark ? <SunIcon /> : <MoonIcon />}
    </button>
  )
}
