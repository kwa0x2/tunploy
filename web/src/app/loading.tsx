import { Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"

export function Loading({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center justify-center", className)}>
      <Loader2 className="text-muted-foreground size-6 animate-spin" />
    </div>
  )
}
