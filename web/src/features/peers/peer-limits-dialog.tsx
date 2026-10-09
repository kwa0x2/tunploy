import { useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { notApplied } from "@/features/peers/save-result"
import type { SaveResult } from "@/features/peers/save-result"
import {
  limitsOf,
  limitsInput,
  fieldErrorsOf,
  badLimitsInput,
} from "@/features/peers/limits"
import type { LimitErrors } from "@/features/peers/limits"
import { LimitFields } from "@/features/peers/limit-fields"
import { api } from "@/api"
import type { Peer } from "@/api"
import {
  countedSince,
  errorMessage,
  formatBytes,
  formatDateTime,
  periodTotal,
} from "@/lib/format"

interface LimitsProps {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: (result: SaveResult) => void
}

export function PeerLimitsDialog({ peer, open, onOpenChange, onSaved }: LimitsProps) {
  const [busy, setBusy] = useState(false)

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Limits for {peer?.name}</DialogTitle>
          <DialogDescription>
            When the data limit or end date is hit the device is disconnected until the limit starts
            again or a later end date. Its config keeps working after that. A speed limit only slows
            it down.
          </DialogDescription>
        </DialogHeader>
        {peer && (
          <PeerLimitsForm
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

function PeerLimitsForm({ peer, busy, setBusy, onDone, onCancel }: {
  peer: Peer
  busy: boolean
  setBusy: (busy: boolean) => void
  onDone: (result: SaveResult) => void
  onCancel: () => void
}) {
  const [limits, setLimits] = useState(() => limitsOf(peer))
  const [error, setError] = useState("")
  const [fieldErrors, setFieldErrors] = useState<LimitErrors>({})

  // For plans that renew on their own date rather than on the 1st.
  async function resetUsage() {
    setError("")
    setBusy(true)
    try {
      onDone({ peer: await api.resetPeerUsage(peer.instance_id, peer.id) })
    } catch (err) {
      const warning = notApplied(err)
      if (warning) {
        onDone({ warning })
        return
      }
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")
    setFieldErrors({})
    const input = limitsInput(limits)
    if (typeof input === "string") {
      setFieldErrors(badLimitsInput(input))
      return
    }
    setBusy(true)
    try {
      onDone({ peer: await api.updatePeer(peer.instance_id, peer.id, input) })
    } catch (err) {
      const warning = notApplied(err)
      if (warning) {
        onDone({ warning })
        return
      }
      const fields = fieldErrorsOf(err)
      if (fields) setFieldErrors(fields)
      else setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <LimitFields value={limits} onChange={setLimits} errors={fieldErrors} />
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="bg-muted/50 flex flex-wrap items-center justify-between gap-2 rounded-lg p-3">
        <p className="text-sm">
          <span className="font-medium tabular-nums">{formatBytes(periodTotal(peer))}</span>{" "}
          <span className="text-muted-foreground">
            counted
            {countedSince(peer)
              ? ` since the reset on ${formatDateTime(countedSince(peer)!)}`
              : peer.limit_period === "monthly"
                ? " this month"
                : " so far"}
          </span>
        </p>
        <Button type="button" size="xs" variant="outline" disabled={busy} onClick={resetUsage}>
          Reset usage
        </Button>
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          Save limits
        </Button>
      </DialogFooter>
    </form>
  )
}
