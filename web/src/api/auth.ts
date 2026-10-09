import { post, request } from "./client"

export interface User {
  id: number
  name: string
  email: string
  totp_enabled: boolean
  created_at: string
}

export interface SetupStatus {
  setup_required: boolean
  // The panel's own container; sent only while there is no admin.
  container?: string
}

export interface Credentials {
  email: string
  password: string
  code?: string
}

export interface TOTPSetup {
  secret: string
  uri: string
}

export interface PasswordChange {
  current_password: string
  new_password: string
}

export const authApi = {
  setupStatus: () => request<SetupStatus>("/api/setup"),
  login: (creds: Credentials) => post<User>("/api/auth/login", creds),
  logout: () => post<void>("/api/auth/logout"),
  me: () => request<User>("/api/auth/me"),
  changePassword: (change: PasswordChange) => post<void>("/api/auth/password", change),
  totpSetup: () => post<TOTPSetup>("/api/auth/totp/setup"),
  totpEnable: (input: { secret: string; code: string; password: string }) =>
    post<User>("/api/auth/totp/enable", input),
  totpDisable: (input: { code: string; password: string }) =>
    post<User>("/api/auth/totp/disable", input),
}
