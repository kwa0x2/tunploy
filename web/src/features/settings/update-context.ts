import { createContext, useContext } from "react"
import type { UpdateStatus } from "@/api"

export interface UpdateState {
  status?: UpdateStatus
  error: unknown
  reload: () => Promise<void>
  check: () => Promise<UpdateStatus>
  // Opens the one dialog that runs an update, from wherever it's offered.
  startUpdate: () => void
}

export const UpdateContext = createContext<UpdateState | null>(null)

export function useUpdate() {
  const ctx = useContext(UpdateContext)
  if (!ctx) throw new Error("useUpdate must be used inside UpdateProvider")
  return ctx
}
