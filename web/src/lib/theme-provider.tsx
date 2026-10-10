import { useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"
import { ThemeContext } from "@/lib/theme"
import type { Theme, ThemeState } from "@/lib/theme"

const STORAGE_KEY = "tunploy-theme"

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
