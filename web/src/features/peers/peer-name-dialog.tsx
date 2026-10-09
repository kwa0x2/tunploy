import { useState } from "react"
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
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/form-field"
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
import { ApiError, api } from "@/api"
import type { Peer } from "@/api"
import { errorMessage } from "@/lib/format"

interface NameProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  peer?: Peer
  onSaved: (result: SaveResult) => void
}

export function PeerNameDialog({ open, onOpenChange, instanceId, peer, onSaved }: NameProps) {
  const [busy, setBusy] = useState(false)

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{peer ? "Rename peer" : "Add peer"}</DialogTitle>
          <DialogDescription>
            {peer
              ? "The name only shows up here and in the config file name."
              : "A peer is one device. It gets its own address and keys."}
          </DialogDescription>
        </DialogHeader>
        <PeerNameForm
          instanceId={instanceId}
          peer={peer}
          busy={busy}
          setBusy={setBusy}
          onDone={(result) => {
            onSaved(result)
            onOpenChange(false)
          }}
          onCancel={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  )
}

function PeerNameForm({ instanceId, peer, busy, setBusy, onDone, onCancel }: {
  instanceId: number
  peer?: Peer
  busy: boolean
  setBusy: (busy: boolean) => void
  onDone: (result: SaveResult) => void
  onCancel: () => void
}) {
  const [name, setName] = useState(peer?.name ?? "")
  const [limits, setLimits] = useState(limitsOf())
  const [error, setError] = useState("")
  const [limitErrors, setLimitErrors] = useState<LimitErrors>({})

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")
    setLimitErrors({})
    const input = peer ? {} : limitsInput(limits)
    if (typeof input === "string") {
      setLimitErrors(badLimitsInput(input))
      return
    }
    setBusy(true)
    try {
      const saved = peer
        ? await api.updatePeer(instanceId, peer.id, { name })
        : await api.createPeer(instanceId, { name, ...input })
      onDone({ peer: saved })
    } catch (err) {
      const warning = notApplied(err)
      if (warning) {
        onDone({ warning })
        return
      }
      const fields = fieldErrorsOf(err)
      if (fields) setLimitErrors(fields)
      else setError(err instanceof ApiError ? (err.fields.name ?? err.message) : errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <FormField id="peer-name" label="Name" error={error}>
        <Input
          id="peer-name"
          placeholder="Alice's phone"
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-invalid={Boolean(error)}
        />
      </FormField>
      {!peer && <LimitFields value={limits} onChange={setLimits} errors={limitErrors} />}
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          {peer ? "Save" : "Add peer"}
        </Button>
      </DialogFooter>
    </form>
  )
}
