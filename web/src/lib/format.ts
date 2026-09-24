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

export function isOnline(peer: Peer): boolean {
  return peer.stats?.online ?? false
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
