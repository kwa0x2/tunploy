import { del, fetchText, patch, post, request } from "./client"
import { instancePath } from "./instances"

export interface PeerStats {
  endpoint?: string
  latest_handshake?: string
  rx_bytes: number
  tx_bytes: number
  online: boolean
}

export interface Traffic {
  rx_bytes: number
  tx_bytes: number
}

export type PeerBlock = "limit" | "expired"

// monthly starts again each calendar month; total only on a usage reset.
export type LimitPeriod = "monthly" | "total"

export interface Peer {
  id: number
  instance_id: number
  name: string
  address: string
  public_key: string
  enabled: boolean
  // Bytes per limit period, both directions; 0 means no limit.
  data_limit: number
  limit_period: LimitPeriod
  usage_reset_at?: string
  expires_at?: string
  // kbit/s each way; 0 means no limit.
  speed_limit: number
  last_handshake?: string
  // Set by an API client for its own user.
  external_id?: string
  // The client made its key pair, so the panel has no private key to show.
  key_on_client?: boolean
  created_at: string
  updated_at: string
  stats?: PeerStats
  country?: string
  month_usage: Traffic
  // What counts toward data_limit.
  period_usage: Traffic
  blocked?: PeerBlock
}

// null clears the expiry.
export type PeerInput = {
  name?: string
  enabled?: boolean
  data_limit?: number
  limit_period?: LimitPeriod
  expires_at?: string | null
  speed_limit?: number
}

export interface ShareLink {
  url: string
  // null keeps the link until it is removed.
  expires_at: string | null
}

export type DeviceStatus = "active" | "disabled" | "expired" | "limit_reached"

// What a share link's page shows the device's owner, who has no account.
export interface SharedDevice {
  name: string
  country: string
  city: string
  status: DeviceStatus
  online: boolean
  last_handshake: string | null
  data_limit: number
  limit_period: LimitPeriod
  period_usage: Traffic
  usage_reset_at: string | null
  month_usage: Traffic
  expires_at: string | null
  speed_limit: number
  // Missing when the device made its own key pair.
  config?: string
  full_tunnel: boolean
  link_expires_at: string | null
}

export interface UsagePoint extends Traffic {
  start: string
}

export interface PeerUsage {
  daily: UsagePoint[]
  monthly: UsagePoint[]
}

export const peerPath = (instanceId: number, peerId: number) =>
  `${instancePath(instanceId)}/peers/${peerId}`

export const sharedConfigUrl = (token: string, killSwitch = false) =>
  `/api/share/${encodeURIComponent(token)}/config${killSwitch ? "?kill_switch=true" : ""}`

export const peerConfigUrl = (instanceId: number, peerId: number, killSwitch = false) =>
  `${peerPath(instanceId, peerId)}/config${killSwitch ? "?kill_switch=true" : ""}`

export const peersApi = {
  peers: (instanceId: number) => request<Peer[]>(`${instancePath(instanceId)}/peers`),
  createPeer: (instanceId: number, input: PeerInput) =>
    post<Peer>(`${instancePath(instanceId)}/peers`, input),
  updatePeer: (instanceId: number, peerId: number, input: PeerInput) =>
    patch<Peer>(peerPath(instanceId, peerId), input),
  deletePeer: (instanceId: number, peerId: number) => del(peerPath(instanceId, peerId)),
  peerConfig: (instanceId: number, peerId: number) =>
    fetchText(peerConfigUrl(instanceId, peerId)),
  peerUsage: (instanceId: number, peerId: number) =>
    request<PeerUsage>(`${peerPath(instanceId, peerId)}/usage`),
  resetPeerUsage: (instanceId: number, peerId: number) =>
    post<Peer>(`${peerPath(instanceId, peerId)}/usage/reset`),
  movePeer: (instanceId: number, peerId: number, targetId: number) =>
    post<Peer>(`${peerPath(instanceId, peerId)}/move`, { instance_id: targetId }),
  peerShare: (instanceId: number, peerId: number) =>
    request<ShareLink>(`${peerPath(instanceId, peerId)}/share`),
  // Replaces the device's link, so the old one stops working.
  createPeerShare: (instanceId: number, peerId: number, expiresAt: string | null) =>
    post<ShareLink>(`${peerPath(instanceId, peerId)}/share`, expiresAt ? { expires_at: expiresAt } : undefined),
  deletePeerShare: (instanceId: number, peerId: number) => del(`${peerPath(instanceId, peerId)}/share`),
  sharedDevice: (token: string) => request<SharedDevice>(`/api/share/${encodeURIComponent(token)}`),
}
