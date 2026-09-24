import { useState } from "react"

// useTarget drives a dialog about one item. The item outlives the open flag so
// the dialog doesn't go blank while its close animation plays.
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
