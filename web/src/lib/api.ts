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

// log is the container's last output, gone once the server rolls back.
export class DeployError extends ApiError {
  readonly log: string[]

  constructor(body: ApiErrorBody, log: string[] = []) {
    super(502, body)
    this.name = "DeployError"
    this.log = log
  }
}

export interface User {
  id: number
  name: string
  email: string
  totp_enabled: boolean
  created_at: string
}

export interface SetupStatus {
  setup_required: boolean
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

export type InstanceState = "running" | "restarting" | "stopped" | "not_deployed" | "unknown"

export interface InstanceSettings {
  address: string
  listen_port: number
  endpoint: string
  dns: string[]
  mtu: number
  persistent_keepalive: number
  client_allowed_ips: string[]
}

export interface Instance extends InstanceSettings {
  id: number
  name: string
  public_key: string
  created_at: string
  updated_at: string
  status: { state: InstanceState; error?: string }
  peer_count: number
}

export type InstanceInput = Partial<InstanceSettings> & { name?: string }

export type ProvisionStep = "image" | "container" | "interface" | "firewall" | "nat"

interface ProvisionEvent {
  step?: ProvisionStep
  instance?: Instance
  error?: ApiErrorBody
  log?: string[]
}

export interface PeerStats {
  endpoint?: string
  latest_handshake?: string
  rx_bytes: number
  tx_bytes: number
  online: boolean
}

export interface Peer {
  id: number
  instance_id: number
  name: string
  address: string
  public_key: string
  enabled: boolean
  created_at: string
  updated_at: string
  stats?: PeerStats
  country?: string
}

export type EventCategory = "connection" | "change" | "auth"

export interface ActivityEvent {
  id: number
  created_at: string
  kind: string
  instance_id?: number
  instance_name?: string
  peer_id?: number
  peer_name?: string
  ip?: string
  country?: string
  detail?: string
}

export interface DockerStatus {
  available: boolean
  error?: string
}

export type PeerInput = { name?: string; enabled?: boolean }

export interface Settings {
  public_host: string
  default_dns: string[]
  public_host_env: string
}

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

export type SettingsInput = Partial<Pick<Settings, "public_host" | "default_dns">>

export interface PasswordChange {
  current_password: string
  new_password: string
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

const patch = <T,>(path: string, body: unknown) =>
  request<T>(path, { method: "PATCH", body: JSON.stringify(body) })

const del = (path: string) => request<void>(path, { method: "DELETE" })

const instancePath = (id: number) => `/api/instances/${id}`
const peerPath = (instanceId: number, peerId: number) =>
  `${instancePath(instanceId)}/peers/${peerId}`

export const peerConfigUrl = (instanceId: number, peerId: number) =>
  `${peerPath(instanceId, peerId)}/config`

async function fetchRaw(path: string, init?: RequestInit): Promise<Response> {
  let res: Response
  try {
    res = await fetch(path, { credentials: "same-origin", ...init })
  } catch (err) {
    if (init?.signal?.aborted) throw err
    throw new ApiError(0, { code: "network_error", message: "Cannot reach the server." })
  }
  if (!res.ok) {
    const body = await res
      .json()
      .then((p: { error?: ApiErrorBody }) => p.error)
      .catch(() => undefined)
    throw new ApiError(
      res.status,
      body ?? { code: "unknown_error", message: `Request failed with status ${res.status}` },
    )
  }
  return res
}

const fetchText = async (path: string) => (await fetchRaw(path)).text()

async function streamText(path: string, onChunk: (text: string) => void, init?: RequestInit) {
  const res = await fetchRaw(path, init)
  if (!res.body) return
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  for (;;) {
    const { done, value } = await reader.read()
    if (done) return
    onChunk(value)
  }
}

async function provisionInstance(
  input: InstanceInput,
  onStep: (step: ProvisionStep) => void,
): Promise<Instance> {
  let created: Instance | undefined
  let rest = ""

  const handle = (line: string) => {
    if (!line.trim()) return
    let event: ProvisionEvent
    try {
      event = JSON.parse(line) as ProvisionEvent
    } catch {
      throw new ApiError(0, {
        code: "invalid_response",
        message: "The server returned a response we could not read.",
      })
    }
    if (event.step) onStep(event.step)
    if (event.error) throw new DeployError(event.error, event.log)
    if (event.instance) created = event.instance
  }

  await streamText(
    "/api/instances",
    (chunk) => {
      const lines = (rest + chunk).split("\n")
      rest = lines.pop() ?? ""
      lines.forEach(handle)
    },
    {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/x-ndjson" },
      body: JSON.stringify(input),
    },
  )
  handle(rest)

  if (!created) {
    throw new ApiError(0, {
      code: "stream_ended",
      message: "The connection closed before the deploy finished. Check the server list.",
    })
  }
  return created
}

export const api = {
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

  settings: () => request<Settings>("/api/settings"),
  updateSettings: (input: SettingsInput) => patch<Settings>("/api/settings", input),
  domain: () => request<DomainStatus>("/api/settings/domain"),
  setDomain: (input: { domain: string; email: string }) =>
    request<DomainStatus>("/api/settings/domain", { method: "PUT", body: JSON.stringify(input) }),
  retryDomain: () => post<DomainStatus>("/api/settings/domain/retry"),
  httpsUrl: () => request<{ url?: string }>("/api/https").then((r) => r.url ?? ""),

  dockerStatus: () => request<DockerStatus>("/api/system/docker"),
  events: (opts: { limit: number; category?: EventCategory }) =>
    request<ActivityEvent[]>(
      `/api/events?limit=${opts.limit}${opts.category ? `&category=${opts.category}` : ""}`,
    ),

  instances: () => request<Instance[]>("/api/instances"),
  instanceDefaults: () => request<InstanceSettings>("/api/instances/defaults"),
  instance: (id: number) => request<Instance>(instancePath(id)),
  provisionInstance,
  updateInstance: (id: number, input: InstanceInput) => patch<Instance>(instancePath(id), input),
  deleteInstance: (id: number) => del(instancePath(id)),
  instanceAction: (id: number, action: "start" | "stop" | "restart") =>
    post<Instance>(`${instancePath(id)}/${action}`),
  instanceLogs: (
    id: number,
    opts: { tail: number; follow: boolean },
    onChunk: (text: string) => void,
    signal: AbortSignal,
  ) =>
    streamText(
      `${instancePath(id)}/logs?tail=${opts.tail}&follow=${opts.follow ? 1 : 0}`,
      onChunk,
      { signal },
    ),

  peers: (instanceId: number) => request<Peer[]>(`${instancePath(instanceId)}/peers`),
  createPeer: (instanceId: number, input: PeerInput) =>
    post<Peer>(`${instancePath(instanceId)}/peers`, input),
  updatePeer: (instanceId: number, peerId: number, input: PeerInput) =>
    patch<Peer>(peerPath(instanceId, peerId), input),
  deletePeer: (instanceId: number, peerId: number) => del(peerPath(instanceId, peerId)),
  peerConfig: (instanceId: number, peerId: number) =>
    fetchText(peerConfigUrl(instanceId, peerId)),
}
