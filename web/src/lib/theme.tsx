import { createContext, useContext, useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"

type Theme = "light" | "dark" | "system"

const STORAGE_KEY = "tunploy-theme"

interface ThemeState {
  theme: Theme
  resolved: "light" | "dark"
  setTheme: (theme: Theme) => void
}

const ThemeContext = createContext<ThemeState | null>(null)

function systemTheme(): "light" | "dark" {
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
}

function storedTheme(): Theme {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === "light" || raw === "dark" || raw === "system") return raw
  } catch {
    // Private browsing blocks storage; fall back to following the system.
  }
  return "system"
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(storedTheme)
  const [resolved, setResolved] = useState<"light" | "dark">(() =>
    theme === "system" ? systemTheme() : theme,
  )

  useEffect(() => {
    const apply = () => {
      const next = theme === "system" ? systemTheme() : theme
      setResolved(next)
      document.documentElement.classList.toggle("dark", next === "dark")
    }
    apply()

    if (theme !== "system") return
    const media = window.matchMedia("(prefers-color-scheme: dark)")
    media.addEventListener("change", apply)
    return () => media.removeEventListener("change", apply)
  }, [theme])

  const value = useMemo<ThemeState>(
    () => ({
      theme,
      resolved,
      setTheme: (next) => {
        setThemeState(next)
        try {
          localStorage.setItem(STORAGE_KEY, next)
        } catch {}
      },
    }),
    [theme, resolved],
  )

  return <ThemeContext value={value}>{children}</ThemeContext>
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error("useTheme must be used inside ThemeProvider")
  return ctx
}
