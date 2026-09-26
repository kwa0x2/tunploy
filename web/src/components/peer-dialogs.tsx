import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { ChevronDown, Download, Loader2, ShieldCheck } from "lucide-react"
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
import type { Instance, LimitPeriod, Peer, PeerInput, ShareLink } from "@/lib/api"
import {
  countedSince,
  errorMessage,
  formatBytes,
  formatDateTime,
  gib,
  locationText,
  periodTotal,
} from "@/lib/format"
import { cn } from "@/lib/utils"

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
      if (err instanceof ApiError && err.code === "apply_failed") {
        onDone({ warning: err.message })
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

type Unit = "MB" | "GB"

const unitBytes: Record<Unit, number> = { MB: 1024 ** 2, GB: gib }

interface Limits {
  size: string
  unit: Unit
  period: LimitPeriod
  until: string
  // Mbit/s
  speed: string
}

const badLimit = "Enter a size, or leave it empty for no limit."
const badSpeed = "Enter a speed up to 10000 Mbit/s, or leave it empty for no limit."

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
    speed: peer?.speed_limit ? String(peer.speed_limit / 1000) : "",
  }
}

type LimitsInput = Pick<PeerInput, "data_limit" | "limit_period" | "expires_at" | "speed_limit">

// Which field is wrong, or the input.
function limitsInput({ size: raw, unit, period, until, speed: rawSpeed }: Limits): LimitsInput | "size" | "speed" {
  const size = raw.trim() === "" ? 0 : Number(raw)
  if (!Number.isFinite(size) || size < 0) return "size"
  const speed = rawSpeed.trim() === "" ? 0 : Math.round(Number(rawSpeed) * 1000)
  if (!Number.isFinite(speed) || speed < 0 || speed > 10_000_000) return "speed"
  let expires: string | null = null
  if (until) {
    const [y, m, d] = until.split("-").map(Number)
    expires = new Date(y, m - 1, d + 1).toISOString()
  }
  return {
    data_limit: Math.round(size * unitBytes[unit]),
    limit_period: period,
    expires_at: expires,
    speed_limit: speed,
  }
}

// A field's error from the server, as LimitFields shows them.
function fieldErrorsOf(err: unknown): LimitErrors | undefined {
  if (!(err instanceof ApiError)) return undefined
  const { data_limit: size, speed_limit: speed } = err.fields
  return size || speed ? { size, speed } : undefined
}

type LimitErrors = { size?: string; speed?: string }

function badLimitsInput(which: "size" | "speed"): LimitErrors {
  return which === "size" ? { size: badLimit } : { speed: badSpeed }
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

function LimitFields({ value, onChange, errors }: {
  value: Limits
  onChange: (value: Limits) => void
  errors: LimitErrors
}) {
  const error = errors.size
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
        id="peer-speed"
        label="Speed limit"
        error={errors.speed}
        hint="Download and upload each. The device stays connected, only slower. Leave empty for no limit."
      >
        <div className="flex items-center gap-1.5">
          <Input
            id="peer-speed"
            type="number"
            min={0}
            step="any"
            inputMode="decimal"
            placeholder="No limit"
            value={value.speed}
            onChange={(e) => onChange({ ...value, speed: e.target.value })}
            aria-invalid={Boolean(errors.speed)}
          />
          <span className="text-muted-foreground shrink-0 text-sm">Mbit/s</span>
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
      if (err instanceof ApiError && err.code === "apply_failed") {
        onDone({ warning: err.message })
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
  // Whether the server's clients send everything through it; a kill switch needs that.
  fullTunnel: boolean
}

export function PeerConfigDialog({ peer, open, onOpenChange, fullTunnel }: ConfigProps) {
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
        {peer && <PeerConfig key={peer.id} peer={peer} fullTunnel={fullTunnel} />}
      </DialogContent>
    </Dialog>
  )
}

function PeerConfig({ peer, fullTunnel }: { peer: Peer; fullTunnel: boolean }) {
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
      <KillSwitchHelp peer={peer} fullTunnel={fullTunnel} />
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

// WireGuard's apps each have their own switch; only wg-quick on Linux needs
// it written into the config.
function KillSwitchHelp({ peer, fullTunnel }: { peer: Peer; fullTunnel: boolean }) {
  return (
    <details className="group rounded-lg border p-3 text-sm">
      <summary className="flex cursor-pointer list-none items-center justify-between font-medium">
        <span className="flex items-center gap-2">
          <ShieldCheck className="size-4" />
          Kill switch
        </span>
        <ChevronDown className="text-muted-foreground size-4 transition-transform group-open:rotate-180" />
      </summary>
      {fullTunnel ? (
        <div className="mt-3 space-y-2">
          <p className="text-muted-foreground">
            Blocks the device&apos;s internet while the VPN is down, so nothing leaks past it.
          </p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
            <dt className="font-medium">Windows</dt>
            <dd className="text-muted-foreground">
              On by default: &quot;Block untunneled traffic&quot; in the tunnel&apos;s settings.
            </dd>
            <dt className="font-medium">Android</dt>
            <dd className="text-muted-foreground">
              Settings → Network → VPN → WireGuard ⚙ → Always-on VPN and Block connections without
              VPN.
            </dd>
            <dt className="font-medium">iPhone, Mac</dt>
            <dd className="text-muted-foreground">
              Turn on On-Demand for the tunnel in the WireGuard app, so it reconnects on every
              network. Apple has no full block for WireGuard.
            </dd>
            <dt className="font-medium">Linux</dt>
            <dd className="text-muted-foreground">
              Use the download below with <code className="font-mono text-xs">wg-quick</code>; it adds
              firewall rules while the tunnel is up.
            </dd>
          </dl>
          <Button
            variant="outline"
            size="sm"
            nativeButton={false}
            render={<a href={peerConfigUrl(peer.instance_id, peer.id, true)} download />}
          >
            <Download />
            Download for Linux with kill switch
          </Button>
        </div>
      ) : (
        <p className="text-muted-foreground mt-3">
          A kill switch blocks everything outside the VPN, but this server only carries some
          traffic. Set its client allowed IPs to 0.0.0.0/0 to use one.
        </p>
      )}
    </details>
  )
}

interface ShareProps {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function PeerShareDialog({ peer, open, onOpenChange }: ShareProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Share {peer?.name}</DialogTitle>
          <DialogDescription>
            A page where the device&apos;s owner scans its QR code and sees how much data is left,
            without signing in. It always shows the current config, even after a move. Anyone with
            the link can use the config, so send it only to them.
          </DialogDescription>
        </DialogHeader>
        {peer && <PeerShare key={peer.id} peer={peer} />}
      </DialogContent>
    </Dialog>
  )
}

const linkDurations = [
  { label: "1 day", days: 1 },
  { label: "1 week", days: 7 },
  { label: "1 month", days: 30 },
  { label: "Until removed", days: 0 },
]

function PeerShare({ peer }: { peer: Peer }) {
  // undefined while loading, null when there is none.
  const [link, setLink] = useState<ShareLink | null>()
  const [days, setDays] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    api
      .peerShare(peer.instance_id, peer.id)
      .then(setLink)
      .catch((err) => {
        if (err instanceof ApiError && err.status === 404) setLink(null)
        else setError(errorMessage(err))
      })
  }, [peer.instance_id, peer.id])

  async function run(action: () => Promise<ShareLink | null>) {
    setError("")
    setBusy(true)
    try {
      setLink(await action())
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const create = () =>
    run(() =>
      api.createPeerShare(
        peer.instance_id,
        peer.id,
        days ? new Date(Date.now() + days * 86_400_000).toISOString() : null,
      ),
    )
  const remove = () => run(() => api.deletePeerShare(peer.instance_id, peer.id).then(() => null))
  const expired = link?.expires_at && new Date(link.expires_at) <= new Date()

  return (
    <div className="space-y-4">
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {link === undefined && !error && <Skeleton className="h-8" />}
      {link && (
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5">
            <Input
              readOnly
              value={link.url}
              aria-label="Share link"
              onFocus={(e) => e.target.select()}
              className="font-mono text-xs"
            />
            <CopyButton value={link.url} label="Copy link" />
          </div>
          <p className={cn("text-xs", expired ? "text-red-700 dark:text-red-400" : "text-muted-foreground")}>
            {link.expires_at
              ? `${expired ? "Stopped working" : "Works until"} ${formatDateTime(link.expires_at)}.`
              : "Works until you remove it."}
          </p>
        </div>
      )}
      {link === null && <p className="text-muted-foreground text-sm">This device has no share link yet.</p>}
      {link?.url.startsWith("http://") && (
        <Alert>
          <AlertDescription>
            The link is plain HTTP, so the config travels unencrypted. Give the panel a domain under
            Settings → Domain and make a new link.
          </AlertDescription>
        </Alert>
      )}
      {peer.key_on_client && (
        <Alert>
          <AlertDescription>
            This device made its own key pair, so the page shows its usage but no QR code or
            config.
          </AlertDescription>
        </Alert>
      )}
      {link !== undefined && (
        <FormField
          id="share-duration"
          label={link ? "A new link works for" : "The link works for"}
          hint={link ? "A new link stops the one above from working." : undefined}
        >
          <div role="radiogroup" aria-label="Link works for" className="flex flex-wrap gap-1.5">
            {linkDurations.map((d) => (
              <Button
                key={d.days}
                type="button"
                size="xs"
                role="radio"
                aria-checked={days === d.days}
                variant={days === d.days ? "default" : "outline"}
                onClick={() => setDays(d.days)}
              >
                {d.label}
              </Button>
            ))}
          </div>
        </FormField>
      )}
      <DialogFooter>
        {link && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void remove()}>
            Remove link
          </Button>
        )}
        <Button type="button" disabled={busy || link === undefined} onClick={() => void create()}>
          {busy && <Loader2 className="animate-spin" />}
          {link ? "Make a new link" : "Create link"}
        </Button>
      </DialogFooter>
    </div>
  )
}
