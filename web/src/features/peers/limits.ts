import { ApiError } from "@/api"
import type { LimitPeriod, Peer, PeerInput } from "@/api"
import { gib } from "@/lib/format"

export type Unit = "MB" | "GB"

const unitBytes: Record<Unit, number> = { MB: 1024 ** 2, GB: gib }

export interface Limits {
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
export function limitsOf(peer?: Peer): Limits {
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
export function limitsInput({ size: raw, unit, period, until, speed: rawSpeed }: Limits): LimitsInput | "size" | "speed" {
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
export function fieldErrorsOf(err: unknown): LimitErrors | undefined {
  if (!(err instanceof ApiError)) return undefined
  const { data_limit: size, speed_limit: speed } = err.fields
  return size || speed ? { size, speed } : undefined
}

export type LimitErrors = { size?: string; speed?: string }

export function badLimitsInput(which: "size" | "speed"): LimitErrors {
  return which === "size" ? { size: badLimit } : { speed: badSpeed }
}

export function presetDate(days: number, months = 0) {
  const now = new Date()
  return dateInput(new Date(now.getFullYear(), now.getMonth() + months, now.getDate() + days))
}
