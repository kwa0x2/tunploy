export interface ApiErrorBody {
  code: string
  message: string
  fields?: Record<string, string>
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fields: Record<string, string>

  constructor(status: number, body: ApiErrorBody) {
    super(body.message)
    this.name = "ApiError"
    this.status = status
    this.code = body.code
    this.fields = body.fields ?? {}
  }
}

export interface User {
  id: number
  name: string
  email: string
  created_at: string
}

export interface SetupStatus {
  setup_required: boolean
}

export interface Credentials {
  email: string
  password: string
}

export interface Registration extends Credentials {
  name: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      credentials: "same-origin",
      headers: init?.body ? { "Content-Type": "application/json" } : undefined,
      ...init,
    })
  } catch {
    throw new ApiError(0, {
      code: "network_error",
      message: "Cannot reach the server. Is Tunploy running?",
    })
  }

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let payload: unknown = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      throw new ApiError(res.status, {
        code: "invalid_response",
        message: "The server returned a response we could not read.",
      })
    }
  }

  if (!res.ok) {
    const body = (payload as { error?: ApiErrorBody } | null)?.error
    throw new ApiError(
      res.status,
      body ?? { code: "unknown_error", message: `Request failed with status ${res.status}` },
    )
  }

  return payload as T
}

const post = <T,>(path: string, body?: unknown) =>
  request<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined })

export const api = {
  setupStatus: () => request<SetupStatus>("/api/setup"),
  setup: (reg: Registration) => post<User>("/api/setup", reg),
  login: (creds: Credentials) => post<User>("/api/auth/login", creds),
  logout: () => post<void>("/api/auth/logout"),
  me: () => request<User>("/api/auth/me"),
}
