import { request } from "./client"

export type EventCategory = "connection" | "change" | "auth"

export interface ActivityEvent {
  id: number
  created_at: string
  kind: string
  instance_id?: number
  instance_name?: string
  peer_id?: number
  peer_name?: string
  node_name?: string
  ip?: string
  country?: string
  detail?: string
  // "api:<key name>" when an API key made the change.
  actor?: string
}

export const eventsApi = {
  events: (opts: { limit: number; category?: EventCategory }) =>
    request<ActivityEvent[]>(
      `/api/events?limit=${opts.limit}${opts.category ? `&category=${opts.category}` : ""}`,
    ),
}
