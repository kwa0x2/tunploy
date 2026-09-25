import { createContext, useCallback, useContext, useMemo, useState } from "react"
import type { ReactNode } from "react"
import { UpdateDialog } from "@/components/update-dialog"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import type { UpdateStatus } from "@/lib/api"

interface UpdateState {
  status?: UpdateStatus
  error: unknown
  reload: () => Promise<void>
  check: () => Promise<UpdateStatus>
  // Opens the one dialog that runs an update, from wherever it's offered.
  startUpdate: () => void
}

const UpdateContext = createContext<UpdateState | null>(null)

const pollMs = 15 * 60 * 1000

export function UpdateProvider({ children }: { children: ReactNode }) {
  const resource = useResource(useCallback(() => api.updateStatus(), []), pollMs)
  const [dialogOpen, setDialogOpen] = useState(false)
  const { reload } = resource

  const check = useCallback(async () => {
    const st = await api.checkUpdate()
    await reload()
    return st
  }, [reload])

  const status = resource.data
  const value = useMemo<UpdateState>(
    () => ({
      status,
      error: resource.error,
      reload,
      check,
      startUpdate: () => setDialogOpen(true),
    }),
    [status, resource.error, reload, check],
  )

  return (
    <UpdateContext.Provider value={value}>
      {children}
      {status?.latest && (
        <UpdateDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          current={status.current}
          target={status.latest.version}
          onFailed={() => void reload()}
        />
      )}
    </UpdateContext.Provider>
  )
}

export function useUpdate() {
  const ctx = useContext(UpdateContext)
  if (!ctx) throw new Error("useUpdate must be used inside UpdateProvider")
  return ctx
}
