import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { FormField } from "@/components/form-field"
import { notApplied } from "@/features/peers/save-result"
import type { SaveResult } from "@/features/peers/save-result"
import { ApiError, api } from "@/api"
import type { Instance, Peer } from "@/api"
import { errorMessage, locationText } from "@/lib/format"

interface MoveProps {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: (result: SaveResult) => void
}

export function PeerMoveDialog({ peer, open, onOpenChange, onSaved }: MoveProps) {
  const [busy, setBusy] = useState(false)

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Move {peer?.name}</DialogTitle>
          <DialogDescription>
            The device keeps its keys, limits and usage history, and gets an address on the new
            server. Its current config stops working, so give it the new one.
          </DialogDescription>
        </DialogHeader>
        {peer && (
          <PeerMoveForm
            key={peer.id}
            peer={peer}
            busy={busy}
            setBusy={setBusy}
            onDone={(result) => {
              onSaved(result)
              onOpenChange(false)
            }}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function PeerMoveForm({ peer, busy, setBusy, onDone, onCancel }: {
  peer: Peer
  busy: boolean
  setBusy: (busy: boolean) => void
  onDone: (result: SaveResult) => void
  onCancel: () => void
}) {
  const [servers, setServers] = useState<Instance[]>()
  const [target, setTarget] = useState(0)
  const [error, setError] = useState("")

  useEffect(() => {
    api
      .instances()
      .then((all) => {
        const others = all.filter((i) => i.id !== peer.instance_id)
        setServers(others)
        setTarget(others[0]?.id ?? 0)
      })
      .catch((err) => setError(errorMessage(err)))
  }, [peer.instance_id])

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    if (!target) return
    setError("")
    setBusy(true)
    try {
      onDone({ peer: await api.movePeer(peer.instance_id, peer.id, target) })
    } catch (err) {
      const warning = notApplied(err)
      if (warning) {
        onDone({ warning })
        return
      }
      setError(err instanceof ApiError ? (err.fields.name ?? err.message) : errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  if (servers?.length === 0) {
    return (
      <>
        <p className="text-muted-foreground text-sm">
          There is no other server to move to. Create one first.
        </p>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel}>
            Close
          </Button>
        </DialogFooter>
      </>
    )
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <FormField id="move-target" label="Server" error={error}>
        {servers ? (
          <select
            id="move-target"
            value={target}
            onChange={(e) => setTarget(Number(e.target.value))}
            className="border-input focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 h-8 w-full rounded-lg border bg-transparent px-2 text-base outline-none focus-visible:ring-3 md:text-sm"
          >
            {servers.map((s) => (
              <option key={s.id} value={s.id}>
                {[s.name, locationText(s)].filter(Boolean).join(" · ")} ({s.peer_count}{" "}
                {s.peer_count === 1 ? "device" : "devices"}
                {s.status.state !== "running" && `, ${s.status.state.replace("_", " ")}`})
              </option>
            ))}
          </select>
        ) : (
          <Skeleton className="h-8" />
        )}
      </FormField>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy || !target}>
          {busy && <Loader2 className="animate-spin" />}
          Move device
        </Button>
      </DialogFooter>
    </form>
  )
}
