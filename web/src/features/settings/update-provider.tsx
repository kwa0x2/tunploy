import { useCallback, useMemo, useState } from "react"
import type { ReactNode } from "react"
import { UpdateDialog } from "@/features/settings/update-dialog"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/api"
import { UpdateContext } from "@/features/settings/update-context"
import type { UpdateState } from "@/features/settings/update-context"

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
    <UpdateContext value={value}>
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
    </UpdateContext>
  )
}
