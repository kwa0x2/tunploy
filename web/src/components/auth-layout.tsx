import type { ReactNode } from "react"
import { Logo } from "@/components/logo"
import { ThemeToggle } from "@/components/theme-toggle"
import { cn } from "@/lib/utils"

export function AuthLayout({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className="bg-muted/40 relative flex min-h-svh items-center justify-center overflow-hidden p-6">
      <div aria-hidden className="pointer-events-none absolute inset-0">
        <div className="absolute -top-40 -left-32 size-[28rem] rounded-full bg-yellow-300/30 blur-3xl dark:bg-yellow-400/15" />
        <div className="absolute top-1/3 -right-40 size-[26rem] rounded-full bg-amber-300/25 blur-3xl dark:bg-amber-500/10" />
        <div className="absolute -bottom-40 left-1/4 size-[24rem] rounded-full bg-orange-300/20 blur-3xl dark:bg-orange-500/10" />
      </div>
      <div className="absolute top-4 right-4">
        <ThemeToggle />
      </div>
      <div className={cn("relative w-full max-w-md space-y-6", className)}>
        <div className="space-y-2 text-center">
          <Logo className="justify-center" />
          <p className="text-muted-foreground text-sm">Your own WireGuard VPN, one click away.</p>
        </div>
        {children}
      </div>
    </div>
  )
}
