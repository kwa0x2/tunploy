import { patch, post, put, request } from "./client"

export interface Settings {
  public_host: string
  default_dns: string[]
  public_host_env: string
}

export type SettingsInput = Partial<Pick<Settings, "public_host" | "default_dns">>

export type DomainState = "off" | "pending" | "ready" | "failed"

export interface DomainStatus {
  enabled: boolean
  domain: string
  email: string
  state: DomainState
  expires?: string
  error?: string
  checked_at?: string
}

export type SMTPSecurity = "starttls" | "tls" | "none"

export type NotificationGroup =
  | "servers"
  | "devices"
  | "limits"
  | "failed_logins"
  | "security"
  | "backups"
  | "logins"
  | "connections"

export interface NotificationSettings {
  enabled: boolean
  host: string
  port: number
  security: SMTPSecurity
  username: string
  password_set: boolean
  from: string
  to: string[]
  events: NotificationGroup[]
  status: { last_sent_at?: string; last_error?: string; last_error_at?: string }
}

// Leaving password out keeps the saved one.
export type NotificationInput = Omit<NotificationSettings, "password_set" | "status"> & {
  password?: string
}

export const settingsApi = {
  settings: () => request<Settings>("/api/settings"),
  updateSettings: (input: SettingsInput) => patch<Settings>("/api/settings", input),
  domain: () => request<DomainStatus>("/api/settings/domain"),
  setDomain: (input: { domain: string; email: string }) =>
    put<DomainStatus>("/api/settings/domain", input),
  retryDomain: () => post<DomainStatus>("/api/settings/domain/retry"),
  notifications: () => request<NotificationSettings>("/api/settings/notifications"),
  setNotifications: (input: NotificationInput) =>
    put<NotificationSettings>("/api/settings/notifications", input),
  testNotifications: (input: NotificationInput) =>
    post<void>("/api/settings/notifications/test", input),
  httpsUrl: () => request<{ url?: string }>("/api/https").then((r) => r.url ?? ""),
}
