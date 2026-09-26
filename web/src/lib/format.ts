import { ApiError } from "@/lib/api"
import type { Instance, Peer } from "@/lib/api"

const units = ["B", "KB", "MB", "GB", "TB"]

export function formatBytes(bytes: number): string {
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 || value >= 10 ? 0 : 1)} ${units[unit]}`
}

// kbit/s, as the panel stores speed limits.
export function formatSpeed(kbit: number): string {
  return kbit < 1000 ? `${kbit} kbit/s` : `${Number((kbit / 1000).toFixed(2))} Mbit/s`
}

export function formatRelative(iso: string, now = Date.now()): string {
  const seconds = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}

export function isOnline(peer: Peer): boolean {
  return peer.stats?.online ?? false
}

// The live handshake is fresher; the stored one survives restarts and disabling.
export function lastSeen(peer: Peer): string | undefined {
  return peer.stats?.latest_handshake ?? peer.last_handshake
}

export const monthTotal = (peer: Peer) => peer.month_usage.rx_bytes + peer.month_usage.tx_bytes

// What counts toward the data limit: this month, or since the last reset.
export const periodTotal = (peer: Peer) => peer.period_usage.rx_bytes + peer.period_usage.tx_bytes

// The reset the limit counts from; a monthly count has moved past an older one.
export function countedSince(peer: Peer, now = new Date()): string | undefined {
  const at = peer.usage_reset_at
  if (!at) return undefined
  if (peer.limit_period === "monthly" && new Date(at) < new Date(now.getFullYear(), now.getMonth(), 1)) {
    return undefined
  }
  return at
}

export const gib = 1024 ** 3

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

// The panel sets expiries at midnight, which reads better as the day before.
export function formatExpiry(iso: string): string {
  const d = new Date(iso)
  if (d.getHours() || d.getMinutes() || d.getSeconds()) return formatDateTime(iso)
  const day = new Date(d.getTime() - 1).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
  })
  return `end of ${day}`
}

export function nextMonthStart(now = new Date()): Date {
  return new Date(now.getFullYear(), now.getMonth() + 1, 1)
}

const regionNames = new Intl.DisplayNames(["en"], { type: "region" })

export function countryName(code: string): string {
  try {
    return regionNames.of(code) ?? code
  } catch {
    return code
  }
}

export function countryFlag(code: string): string {
  if (!/^[A-Z]{2}$/.test(code)) return ""
  return String.fromCodePoint(...[...code].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}

// "🇩🇪 Frankfurt, Germany", or "" when the server has no location.
export function locationText({ country, city }: { country: string; city?: string }): string {
  const place = [city, country && countryName(country)].filter(Boolean).join(", ")
  return [country && countryFlag(country), place].filter(Boolean).join(" ")
}

export function endpointHost(endpoint: string): string {
  const v6 = endpoint.match(/^\[(.+)\]:\d+$/)
  if (v6) return v6[1].replace(/^::ffff:/, "")
  return endpoint.replace(/:\d+$/, "")
}

export function endpointOf(instance: Instance): string {
  const host = instance.endpoint.includes(":") ? `[${instance.endpoint}]` : instance.endpoint
  return `${host}:${instance.listen_port}`
}

export function errorMessage(err: unknown): string {
  return err instanceof ApiError ? err.message : "Something went wrong. Please try again."
}

// Under Compose or Dokploy the container is not called "tunploy"; when the
// panel can't tell, the admin fills the name in.
export function execCommand(container: string | undefined, args: string): string {
  return `docker exec -it ${container || "CONTAINER_ID"} tunploy ${args}`
}
