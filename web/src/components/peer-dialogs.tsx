import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { Download, Loader2 } from "lucide-react"
import { QRCodeSVG } from "qrcode.react"
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
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { CopyButton } from "@/components/copy-button"
import { FormField } from "@/components/form-field"
import { ApiError, api, peerConfigUrl } from "@/lib/api"
import type { Peer, PeerInput } from "@/lib/api"
import { errorMessage, gib } from "@/lib/format"

interface NameProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  peer?: Peer
  onSaved: (result: SaveResult) => void
}

type SaveResult = { peer?: Peer; warning?: string }

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
  const [limitError, setLimitError] = useState("")

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")
    setLimitError("")
    const input = peer ? {} : limitsInput(limits)
    if (!input) {
      setLimitError(badLimit)
      return
    }
    setBusy(true)
    try {
      const saved = peer
        ? await api.updatePeer(instanceId, peer.id, { name })
        : await api.createPeer(instanceId, { name, ...input })
      onDone({ peer: saved })
    } catch (err) {
      if (err instanceof ApiError && err.code === "apply_failed") {
        onDone({ warning: err.message })
        return
      }
      if (err instanceof ApiError && err.fields.data_limit) setLimitError(err.fields.data_limit)
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
      {!peer && <LimitFields value={limits} onChange={setLimits} error={limitError} />}
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

type Unit = "MB" | "GB"

const unitBytes: Record<Unit, number> = { MB: 1024 ** 2, GB: gib }

interface Limits {
  size: string
  unit: Unit
  until: string
}

const badLimit = "Enter a size, or leave it empty for no limit."

const pad = (n: number) => String(n).padStart(2, "0")
const dateInput = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`

// Access runs to the end of the chosen day, so the stored instant is the next midnight.
// Limits under 1 GB read better in MB.
function limitsOf(peer?: Peer): Limits {
  const limit = peer?.data_limit ?? 0
  const unit: Unit = limit && limit < gib ? "MB" : "GB"
  return {
    size: limit ? String(Number((limit / unitBytes[unit]).toFixed(2))) : "",
    unit,
    until: peer?.expires_at ? dateInput(new Date(new Date(peer.expires_at).getTime() - 1)) : "",
  }
}

function limitsInput({ size: raw, unit, until }: Limits): Pick<PeerInput, "data_limit" | "expires_at"> | null {
  const size = raw.trim() === "" ? 0 : Number(raw)
  if (!Number.isFinite(size) || size < 0) return null
  let expires: string | null = null
  if (until) {
    const [y, m, d] = until.split("-").map(Number)
    expires = new Date(y, m - 1, d + 1).toISOString()
  }
  return { data_limit: Math.round(size * unitBytes[unit]), expires_at: expires }
}

function presetDate(days: number, months = 0) {
  const now = new Date()
  return dateInput(new Date(now.getFullYear(), now.getMonth() + months, now.getDate() + days))
}

const presets = [
  { label: "Today", date: () => presetDate(0) },
  { label: "1 week", date: () => presetDate(7) },
  { label: "1 month", date: () => presetDate(0, 1) },
]

function LimitFields({ value, onChange, error }: {
  value: Limits
  onChange: (value: Limits) => void
  error?: string
}) {
  return (
    <>
      <FormField
        id="peer-limit"
        label="Monthly data limit"
        error={error}
        hint="Download and upload together. Resets on the 1st. Leave empty for no limit."
      >
        <div className="flex gap-1.5">
          <Input
            id="peer-limit"
            type="number"
            min={0}
            step="any"
            inputMode="decimal"
            placeholder="No limit"
            value={value.size}
            onChange={(e) => onChange({ ...value, size: e.target.value })}
            aria-invalid={Boolean(error)}
          />
          <div role="radiogroup" aria-label="Unit" className="flex shrink-0 gap-1">
            {(["MB", "GB"] as const).map((unit) => (
              <Button
                key={unit}
                type="button"
                role="radio"
                aria-checked={value.unit === unit}
                variant={value.unit === unit ? "default" : "outline"}
                onClick={() => onChange({ ...value, unit })}
              >
                {unit}
              </Button>
            ))}
          </div>
        </div>
      </FormField>
      <FormField
        id="peer-until"
        label="Access until"
        hint={value.until ? "The device is cut off when this day ends." : "No end date."}
      >
        <Input
          id="peer-until"
          type="date"
          min={presetDate(0)}
          value={value.until}
          onChange={(e) => onChange({ ...value, until: e.target.value })}
        />
        <div className="flex flex-wrap gap-1.5">
          {presets.map((p) => {
            const date = p.date()
            return (
              <Button
                key={p.label}
                type="button"
                size="xs"
                variant={value.until === date ? "secondary" : "outline"}
                onClick={() => onChange({ ...value, until: date })}
              >
                {p.label}
              </Button>
            )
          })}
          <Button
            type="button"
            size="xs"
            variant={value.until ? "outline" : "secondary"}
            onClick={() => onChange({ ...value, until: "" })}
          >
            Never
          </Button>
        </div>
      </FormField>
    </>
  )
}

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
            When a limit is hit the device is disconnected until the next month or a later end
            date. Its config keeps working after that.
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

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")
    const input = limitsInput(limits)
    if (!input) {
      setError(badLimit)
      return
    }
    setBusy(true)
    try {
      onDone({ peer: await api.updatePeer(peer.instance_id, peer.id, input) })
    } catch (err) {
      if (err instanceof ApiError && err.code === "apply_failed") {
        onDone({ warning: err.message })
        return
      }
      setError(err instanceof ApiError ? (err.fields.data_limit ?? err.message) : errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <LimitFields value={limits} onChange={setLimits} error={error} />
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

interface ConfigProps {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function PeerConfigDialog({ peer, open, onOpenChange }: ConfigProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{peer?.name}</DialogTitle>
          <DialogDescription>
            Scan with the WireGuard app, or download the file for a desktop client. Anyone with
            this config can join the VPN as this peer.
          </DialogDescription>
        </DialogHeader>
        {peer && <PeerConfig key={peer.id} peer={peer} />}
      </DialogContent>
    </Dialog>
  )
}

function PeerConfig({ peer }: { peer: Peer }) {
  const [config, setConfig] = useState<string>()
  const [error, setError] = useState("")

  useEffect(() => {
    api
      .peerConfig(peer.instance_id, peer.id)
      .then(setConfig)
      .catch((err) => setError(errorMessage(err)))
  }, [peer.instance_id, peer.id])

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="space-y-4">
      {!peer.enabled && (
        <Alert>
          <AlertDescription>
            This peer is disabled. The config is valid but the server will refuse it until you
            enable the peer again.
          </AlertDescription>
        </Alert>
      )}
      <div className="flex justify-center">
        {config ? (
          // A white quiet zone keeps the code scannable in dark mode too.
          <div className="rounded-lg bg-white p-3">
            <QRCodeSVG value={config} size={224} marginSize={0} />
          </div>
        ) : (
          <Skeleton className="size-[248px] rounded-lg" />
        )}
      </div>
      <DialogFooter>
        {config && <CopyButton value={config} label="Copy config" showLabel />}
        <Button
          variant="outline"
          nativeButton={false}
          render={<a href={peerConfigUrl(peer.instance_id, peer.id)} download />}
        >
          <Download />
          Download .conf
        </Button>
      </DialogFooter>
    </div>
  )
}
