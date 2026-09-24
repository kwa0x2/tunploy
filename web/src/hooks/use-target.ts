import { useState } from "react"

// The item outlives the open flag so the dialog doesn't blank while closing.
export function useTarget<T>() {
  const [target, setTarget] = useState<T>()
  const [open, setOpen] = useState(false)
  return {
    target,
    open,
    show: (item: T) => {
      setTarget(item)
      setOpen(true)
    },
    onOpenChange: setOpen,
  }
}
