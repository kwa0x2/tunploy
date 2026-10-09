import { del, patch, post, request } from "./client"
import type { Page } from "./client"

export interface Webhook {
  id: number
  url: string
  // Event kinds, or ["*"] for all of them.
  events: string[]
  description: string
  enabled: boolean
  // "api:<key name>" when an API key made it.
  created_by?: string
  created_at: string
  updated_at: string
}

export interface WebhookInput {
  url?: string
  events?: string[]
  description?: string
  enabled?: boolean
}

// secret is shown this once.
export type CreatedWebhook = Webhook & { secret: string }

export type DeliveryState = "pending" | "succeeded" | "failed"

export interface WebhookDelivery {
  id: number
  webhook_id: number
  event_id?: number
  kind: string
  payload: unknown
  state: DeliveryState
  attempts: number
  next_attempt_at?: string
  last_attempt_at?: string
  response_status?: number
  error?: string
  duration_ms?: number
  created_at: string
}

export const webhooksApi = {
  webhooks: () => request<Webhook[]>("/api/webhooks"),
  createWebhook: (input: WebhookInput) => post<CreatedWebhook>("/api/webhooks", input),
  updateWebhook: (id: number, input: WebhookInput) => patch<Webhook>(`/api/webhooks/${id}`, input),
  deleteWebhook: (id: number) => del(`/api/webhooks/${id}`),
  pingWebhook: (id: number) => post<WebhookDelivery>(`/api/webhooks/${id}/ping`),
  webhookDeliveries: (id: number, before?: number) =>
    request<Page<WebhookDelivery>>(
      `/api/webhooks/${id}/deliveries?limit=20${before ? `&before=${before}` : ""}`,
    ),
  retryDelivery: (id: number, deliveryId: number) =>
    post<WebhookDelivery>(`/api/webhooks/${id}/deliveries/${deliveryId}/retry`),
}
