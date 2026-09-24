import { Moon, Sun } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useTheme } from "@/lib/theme"

export function ThemeToggle() {
  const { resolved, setTheme } = useTheme()

  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={resolved === "dark" ? "Switch to light theme" : "Switch to dark theme"}
      onClick={() => setTheme(resolved === "dark" ? "light" : "dark")}
    >
      {resolved === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
    </Button>
  )
}
