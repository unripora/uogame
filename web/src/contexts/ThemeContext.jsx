import React, { createContext, useContext, useEffect, useState } from 'react'
import {
  THEMES,
  DEFAULT_THEME,
  applyTheme,
  loadStoredTheme,
  storeTheme,
} from '../lib/theme'

// 主题上下文：切换 5 套外观并持久化到 localStorage
const ThemeContext = createContext(null)

export function ThemeProvider({ children }) {
  const [theme, setThemeState] = useState(() => loadStoredTheme())

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  const setTheme = (id) => {
    setThemeState(id)
    storeTheme(id)
  }

  return (
    <ThemeContext.Provider value={{ theme, setTheme, themes: THEMES, defaultTheme: DEFAULT_THEME }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider')
  return ctx
}
