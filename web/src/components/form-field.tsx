import type { ReactNode } from "react"
import { Label } from "@/components/ui/label"

interface Props {
  id: string
  label: string
  error?: string
  hint?: ReactNode
  children: ReactNode
}

export function FormField({ id, label, error, hint, children }: Props) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error ? (
        <p className="text-destructive text-sm">{error}</p>
      ) : (
        hint && <p className="text-muted-foreground text-xs">{hint}</p>
      )}
    </div>
  )
}
