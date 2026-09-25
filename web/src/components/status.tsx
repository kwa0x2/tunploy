import { Smartphone } from "lucide-react"
import type { InstanceState, NodeState, Peer, PeerBlock } from "@/lib/api"
import { countryFlag, countryName, formatBytes, monthTotal } from "@/lib/format"
import { cn } from "@/lib/utils"

const states: Record<InstanceState, { label: string; tone: string; dot: string }> = {
  running: {
    label: "Running",
    tone: "bg-emerald-500/10 text-emerald-700 ring-emerald-600/20 dark:text-emerald-400 dark:ring-emerald-400/25",
    dot: "bg-emerald-500 motion-safe:animate-pulse",
  },
  restarting: {
    label: "Crashing",
    tone: "bg-red-500/10 text-red-700 ring-red-600/20 dark:text-red-400 dark:ring-red-400/25",
    dot: "bg-red-500",
  },
  stopped: {
    label: "Stopped",
    tone: "bg-muted text-muted-foreground ring-foreground/10",
    dot: "bg-muted-foreground",
  },
  not_deployed: {
    label: "Not deployed",
    tone: "bg-orange-500/10 text-orange-700 ring-orange-600/20 dark:text-orange-400 dark:ring-orange-400/25",
    dot: "bg-orange-500",
  },
  unknown: {
    label: "Unknown",
    tone: "bg-orange-500/10 text-orange-700 ring-orange-600/20 dark:text-orange-400 dark:ring-orange-400/25",
    dot: "bg-orange-500",
  },
}

export function InstanceStatus({ state, className }: { state: InstanceState; className?: string }) {
  const { label, tone, dot } = states[state]
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
        tone,
        className,
      )}
    >
      <span className={cn("size-1.5 rounded-full", dot)} />
      {label}
    </span>
  )
}

const nodeStates: Record<NodeState, { label: string; tone: string; dot: string }> = {
  online: { ...states.running, label: "Online" },
  connecting: { ...states.unknown, label: "Connecting" },
  offline: { ...states.restarting, label: "Offline" },
}

export function NodeStatus({ state, className }: { state: NodeState; className?: string }) {
  const { label, tone, dot } = nodeStates[state]
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
        tone,
        className,
      )}
    >
      <span className={cn("size-1.5 rounded-full", dot)} />
      {label}
    </span>
  )
}

const blocks: Record<PeerBlock, { label: string; title: string }> = {
  limit: { label: "Limit reached", title: "Used up this month's data; back on the 1st" },
  expired: { label: "Expired", title: "Access has ended; set a later date to let it back in" },
}

export function PeerStatus({ online, enabled, lastSeen, blocked }: {
  online: boolean
  enabled: boolean
  lastSeen?: string
  blocked?: PeerBlock
}) {
  if (!enabled) return <span className="text-muted-foreground text-xs">Disabled</span>
  if (blocked) {
    return (
      <span
        title={blocks[blocked].title}
        className="inline-flex items-center gap-1.5 text-xs font-medium text-red-700 dark:text-red-400"
      >
        <span className="size-1.5 rounded-full bg-red-500" />
        {blocks[blocked].label}
      </span>
    )
  }
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-xs",
        online && "font-medium text-emerald-700 dark:text-emerald-400",
      )}
    >
      <span
        className={cn("size-1.5 rounded-full", online ? "bg-emerald-500" : "bg-muted-foreground/50")}
      />
      {online ? "Online" : lastSeen ? `Seen ${lastSeen}` : "Never connected"}
    </span>
  )
}

// Shown from the device's side: its download is what the server sent.
export function MonthUsage({ peer }: { peer: Peer }) {
  const total = monthTotal(peer)
  const title = `This month: ↓ ${formatBytes(peer.month_usage.tx_bytes)} downloaded · ↑ ${formatBytes(peer.month_usage.rx_bytes)} uploaded`

  if (!peer.data_limit) {
    return (
      <span className="text-xs whitespace-nowrap tabular-nums" title={title}>
        {total ? formatBytes(total) : <span className="text-muted-foreground">—</span>}
      </span>
    )
  }
  return (
    <div className="w-28 space-y-1" title={title}>
      <p className="text-xs whitespace-nowrap tabular-nums">
        {formatBytes(total)}
        <span className="text-muted-foreground"> / {formatBytes(peer.data_limit)}</span>
      </p>
      <UsageMeter used={total} limit={peer.data_limit} />
    </div>
  )
}

export function UsageMeter({ used, limit, className }: { used: number; limit: number; className?: string }) {
  const pct = Math.min(100, (used / limit) * 100)
  return (
    <div
      role="meter"
      aria-label="Share of the monthly data limit used"
      aria-valuenow={Math.round(pct)}
      aria-valuemin={0}
      aria-valuemax={100}
      className={cn("bg-muted h-1.5 overflow-hidden rounded-full", className)}
    >
      <div
        className={cn(
          "h-full rounded-full transition-[width]",
          pct >= 100 ? "bg-red-500" : pct >= 80 ? "bg-amber-500" : "bg-sky-500",
        )}
        style={{ width: `${Math.max(pct, used > 0 ? 2 : 0)}%` }}
      />
    </div>
  )
}

export function Location({ ip, country, className }: { ip?: string; country?: string; className?: string }) {
  if (!ip) return <span className="text-muted-foreground">—</span>
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5 text-xs", className)}>
      {country && (
        <span title={countryName(country)} aria-label={countryName(country)}>
          {countryFlag(country)}
        </span>
      )}
      <span className="min-w-0 truncate">
        {country && <span className="mr-1.5">{countryName(country)}</span>}
        <span className="text-muted-foreground font-mono">{ip}</span>
      </span>
    </span>
  )
}

// DB-IP Lite is CC BY 4.0, which asks for this wherever its data shows.
export function GeoAttribution({ className }: { className?: string }) {
  return (
    <p className={cn("text-muted-foreground text-xs", className)}>
      <a href="https://db-ip.com" target="_blank" rel="noreferrer" className="underline-offset-4 hover:underline">
        IP Geolocation by DB-IP
      </a>
    </p>
  )
}

export function DeviceIcon({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "grid size-7 shrink-0 place-items-center rounded-full bg-sky-500/10 text-sky-600 dark:text-sky-400",
        className,
      )}
    >
      <Smartphone className="size-[55%]" />
    </span>
  )
}
