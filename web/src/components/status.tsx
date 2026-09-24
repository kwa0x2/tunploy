import { Smartphone } from "lucide-react"
import type { InstanceState, PeerStats } from "@/lib/api"
import { countryFlag, countryName, formatBytes } from "@/lib/format"
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

export function PeerStatus({ online, enabled, lastSeen }: {
  online: boolean
  enabled: boolean
  lastSeen?: string
}) {
  if (!enabled) return <span className="text-muted-foreground text-xs">Disabled</span>
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
