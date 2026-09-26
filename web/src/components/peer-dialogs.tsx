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
import type { Instance, LimitPeriod, Peer, PeerInput } from "@/lib/api"
import {
  countedSince,
  errorMessage,
  formatBytes,
  formatDateTime,
  gib,
  locationText,
  periodTotal,
} from "@/lib/format"

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
  period: LimitPeriod
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
    period: peer?.limit_period ?? "monthly",
    until: peer?.expires_at ? dateInput(new Date(new Date(peer.expires_at).getTime() - 1)) : "",
  }
}

function limitsInput({
  size: raw,
  unit,
  period,
  until,
}: Limits): Pick<PeerInput, "data_limit" | "limit_period" | "expires_at"> | null {
  const size = raw.trim() === "" ? 0 : Number(raw)
  if (!Number.isFinite(size) || size < 0) return null
  let expires: string | null = null
  if (until) {
    const [y, m, d] = until.split("-").map(Number)
    expires = new Date(y, m - 1, d + 1).toISOString()
  }
  return { data_limit: Math.round(size * unitBytes[unit]), limit_period: period, expires_at: expires }
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

const periods: { value: LimitPeriod; label: string }[] = [
  { value: "monthly", label: "Every month" },
  { value: "total", label: "In total" },
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
        label="Data limit"
        error={error}
        hint={
          value.period === "monthly"
            ? "Download and upload together. Starts again on the 1st. Leave empty for no limit."
            : "Download and upload together. Counts until you reset the usage. Leave empty for no limit."
        }
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
        <div role="radiogroup" aria-label="Counted" className="flex flex-wrap gap-1.5">
          {periods.map((p) => (
            <Button
              key={p.value}
              type="button"
              size="xs"
              role="radio"
              aria-checked={value.period === p.value}
              variant={value.period === p.value ? "default" : "outline"}
              onClick={() => onChange({ ...value, period: p.value })}
            >
              {p.label}
            </Button>
          ))}
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
            When a limit is hit the device is disconnected until the limit starts again or a later
            end date. Its config keeps working after that.
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

  // For plans that renew on their own date rather than on the 1st.
  async function resetUsage() {
    setError("")
    setBusy(true)
    try {
      onDone({ peer: await api.resetPeerUsage(peer.instance_id, peer.id) })
    } catch (err) {
      if (err instanceof ApiError && err.code === "apply_failed") {
        onDone({ warning: err.message })
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
      if (err instanceof ApiError && err.code === "apply_failed") {
        onDone({ warning: err.message })
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
            {peer?.key_on_client
              ? "The server side of this device's config."
              : "Scan with the WireGuard app, or download the file for a desktop client. Anyone with this config can join the VPN as this peer."}
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
      {peer.key_on_client && (
        <Alert>
          <AlertDescription>
            This device made its own key pair, so the panel never saw its private key. The config
            below has no PrivateKey line; the app that owns the key fills it in.
          </AlertDescription>
        </Alert>
      )}
      <div className="flex justify-center">
        {config && peer.key_on_client ? (
          <pre className="bg-muted max-h-64 w-full overflow-auto rounded-lg p-3 font-mono text-xs">
            {config}
          </pre>
        ) : config ? (
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
