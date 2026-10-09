import { del, post, request } from "./client"

export type ApiScope =
  | "devices:read"
  | "devices:write"
  | "servers:read"
  | "servers:write"
  | "events:read"
  | "webhooks:write"

export interface ApiKey {
  id: number
  name: string
  // The start of the key, to tell keys apart.
  prefix: string
  scopes: ApiScope[]
  expires_at?: string
  last_used_at?: string
  last_used_ip?: string
  created_at: string
}

export interface ApiKeyInput {
  name: string
  scopes: ApiScope[]
  expires_at?: string
}

// token is shown this once; the panel keeps only its hash.
export type CreatedApiKey = ApiKey & { token: string }

export const apiKeysApi = {
  apiKeys: () => request<ApiKey[]>("/api/api-keys"),
  createApiKey: (input: ApiKeyInput) => post<CreatedApiKey>("/api/api-keys", input),
  deleteApiKey: (id: number) => del(`/api/api-keys/${id}`),
}
