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

export function formatRelative(iso: string, now = Date.now()): string {
  const seconds = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}

// The server decides, since it can watch the traffic counters between polls.
export function isOnline(peer: Peer): boolean {
  return peer.stats?.online ?? false
}

export function endpointOf(instance: Instance): string {
  const host = instance.endpoint.includes(":") ? `[${instance.endpoint}]` : instance.endpoint
  return `${host}:${instance.listen_port}`
}

export function errorMessage(err: unknown): string {
  return err instanceof ApiError ? err.message : "Something went wrong. Please try again."
}
