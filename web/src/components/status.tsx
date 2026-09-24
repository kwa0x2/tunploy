import type { InstanceState, PeerStats } from "@/lib/api"
import { formatBytes } from "@/lib/format"
import { cn } from "@/lib/utils"

const states: Record<InstanceState, { label: string; dot: string }> = {
  running: { label: "Running", dot: "bg-emerald-500" },
  restarting: { label: "Crashing", dot: "bg-destructive" },
  stopped: { label: "Stopped", dot: "bg-muted-foreground" },
  not_deployed: { label: "Not deployed", dot: "bg-amber-500" },
  unknown: { label: "Unknown", dot: "bg-amber-500" },
}

export function InstanceStatus({ state, className }: { state: InstanceState; className?: string }) {
  const { label, dot } = states[state]
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium",
        className,
      )}
    >
      <span className={cn("size-1.5 rounded-full", dot)} />
      {label}
    </span>
  )
}

export function PeerStatus({ online, enabled, lastSeen }: {
  online: boolean
  enabled: boolean
  lastSeen?: string
}) {
  if (!enabled) return <span className="text-muted-foreground text-xs">Disabled</span>
  return (
    <span className="inline-flex items-center gap-1.5 text-xs">
      <span
        className={cn("size-1.5 rounded-full", online ? "bg-emerald-500" : "bg-muted-foreground/50")}
      />
      {online ? "Online" : lastSeen ? `Seen ${lastSeen}` : "Never connected"}
    </span>
  )
}

// Traffic is shown from the device's side: what it downloaded is what the
// server sent it.
export function PeerTraffic({ stats }: { stats?: PeerStats }) {
  if (!stats) return <span className="text-muted-foreground">—</span>
  return (
    <span className="text-xs whitespace-nowrap tabular-nums">
      <span title="Downloaded by the device">↓ {formatBytes(stats.tx_bytes)}</span>
      <span className="text-muted-foreground mx-1.5">·</span>
      <span title="Uploaded by the device">↑ {formatBytes(stats.rx_bytes)}</span>
    </span>
  )
}
