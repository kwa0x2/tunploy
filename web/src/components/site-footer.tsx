import { cn } from "@/lib/utils"

export function SiteFooter({ className }: { className?: string }) {
  return (
    <footer className={cn("text-muted-foreground text-center text-xs", className)}>
      <a
        href="https://core.cro.ie/e-commerce/company/5802085"
        target="_blank"
        rel="noreferrer"
        className="hover:text-foreground underline-offset-2 transition-colors hover:underline"
      >
        Netta Technologies
      </a>{" "}
      · © 2026 Alper Karakoyun
    </footer>
  )
}
