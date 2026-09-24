import { useState } from "react"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"

interface Props {
  value: string
  label?: string
  showLabel?: boolean
}

export function CopyButton({ value, label = "Copy", showLabel = false }: Props) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard access needs HTTPS or localhost; a plain-IP panel has neither.
      toast.error("Your browser blocked clipboard access on this connection.")
    }
  }

  const icon = copied ? <Check /> : <Copy />
  if (showLabel) {
    return (
      <Button variant="outline" onClick={() => void copy()}>
        {icon}
        {copied ? "Copied" : label}
      </Button>
    )
  }
  return (
    <Button variant="ghost" size="icon-xs" aria-label={label} onClick={() => void copy()}>
      {icon}
    </Button>
  )
}
