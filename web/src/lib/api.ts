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
  // 0 is the panel's own machine.
  node_id: number
  name: string
  public_key: string
  created_at: string
  updated_at: string
  status: { state: InstanceState; error?: string }
  peer_count: number
}

export type InstanceInput = Partial<InstanceSettings> & { name?: string; node_id?: number }

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

export interface Traffic {
  rx_bytes: number
  tx_bytes: number
}

export type PeerBlock = "limit" | "expired"

export interface Peer {
  id: number
  instance_id: number
  name: string
  address: string
  public_key: string
  enabled: boolean
  // Bytes per calendar month, both directions; 0 means no limit.
  data_limit: number
  expires_at?: string
  last_handshake?: string
  created_at: string
  updated_at: string
  stats?: PeerStats
  country?: string
  month_usage: Traffic
  blocked?: PeerBlock
}

export interface UsagePoint extends Traffic {
  start: string
}

export interface PeerUsage {
  daily: UsagePoint[]
  monthly: UsagePoint[]
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
  node_name?: string
  ip?: string
  country?: string
  detail?: string
}

export interface Release {
  version: string
  url: string
  published_at: string
}

export interface UpdateStatus {
  current: string
  latest?: Release
  available: boolean
  checked_at?: string
  check_error?: string
  // Why the panel can't update itself; absent when it can.
  unsupported?: string
  // The version being installed while an update runs.
  updating?: string
  // Why the last update failed.
  error?: string
}

export interface DockerStatus {
  available: boolean
  error?: string
  container?: string
}

// null clears the expiry.
export type PeerInput = {
  name?: string
  enabled?: boolean
  data_limit?: number
  expires_at?: string | null
}

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

export type BackupSchedule = "off" | "daily" | "weekly"

export interface BackupStatus {
  running: boolean
  last_backup_at?: string
  last_backup_name?: string
  last_backup_size?: number
  last_error?: string
  last_error_at?: string
  next_run_at?: string
}

export interface BackupSettings {
  connected: boolean
  endpoint: string
  region: string
  bucket: string
  prefix: string
  access_key: string
  secret_key_set: boolean
  path_style: boolean
  schedule: BackupSchedule
  hour: number
  // How many backups stay in the bucket; 0 keeps them all.
  keep: number
  // New backups are encrypted with the panel's passphrase.
  encrypted: boolean
  timezone: string
  status: BackupStatus
}

// Leaving secret_key out keeps the saved one.
export type BackupInput = Omit<BackupSettings, "connected" | "secret_key_set" | "encrypted" | "timezone" | "status"> & {
  secret_key?: string
}

export interface BackupObject {
  key: string
  name: string
  size: number
  modified: string
  encrypted: boolean
}

export interface RestoreResult {
  name: string
  created_at: string
  version: string
  warnings: string[]
}

export type NodeState = "online" | "offline" | "connecting"

export interface NodeDaemon {
  version: string
  os: string
  arch: string
  kernel_version: string
  cpus: number
  memory: number
}

export interface Node {
  // 0 is the panel's own machine.
  id: number
  name: string
  host: string
  port: number
  username: string
  host_key: string
  host_key_fingerprint?: string
  last_seen_at?: string
  created_at?: string
  local: boolean
  server_count: number
  status: { state: NodeState; error?: string; since?: string; daemon?: NodeDaemon }
}

export interface HostKeyScan {
  host_key: string
  fingerprint: string
  algorithm: string
}

export interface PanelKey {
  public_key: string
  fingerprint: string
}

// The credentials are used once, to add the panel's own key.
export interface NodeInput {
  name: string
  host: string
  port: number
  username: string
  host_key: string
  password?: string
  private_key?: string
  passphrase?: string
}

export type NodeStep = "connect" | "authorize" | "docker" | "wireguard" | "image"

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

export const backupExportUrl = "/api/backups/export"
export const backupFileUrl = (name: string) => `/api/backups/${encodeURIComponent(name)}`

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

interface StreamEvent<T, S> {
  step?: S
  error?: ApiErrorBody
  log?: string[]
  result?: T
}

// NDJSON: steps as they happen, then the result or an error on the last line.
async function streamSteps<T, S>(
  path: string,
  body: unknown,
  parse: (raw: Record<string, unknown>) => StreamEvent<T, S>,
  onStep: (step: S) => void,
): Promise<T> {
  let result: T | undefined
  let rest = ""

  const handle = (line: string) => {
    if (!line.trim()) return
    let event: StreamEvent<T, S>
    try {
      event = parse(JSON.parse(line) as Record<string, unknown>)
    } catch {
      throw new ApiError(0, {
        code: "invalid_response",
        message: "The server returned a response we could not read.",
      })
    }
    if (event.step) onStep(event.step)
    if (event.error) throw new DeployError(event.error, event.log)
    if (event.result) result = event.result
  }

  await streamText(
    path,
    (chunk) => {
      const lines = (rest + chunk).split("\n")
      rest = lines.pop() ?? ""
      lines.forEach(handle)
    },
    {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/x-ndjson" },
      body: JSON.stringify(body),
    },
  )
  handle(rest)

  if (!result) {
    throw new ApiError(0, {
      code: "stream_ended",
      message: "The connection closed before it finished. Check the list before trying again.",
    })
  }
  return result
}

const provisionInstance = (input: InstanceInput, onStep: (step: ProvisionStep) => void) =>
  streamSteps<Instance, ProvisionStep>(
    "/api/instances",
    input,
    (raw) => {
      const { instance, ...rest } = raw as ProvisionEvent
      return { ...rest, result: instance }
    },
    onStep,
  )

const addNode = (input: NodeInput, onStep: (step: NodeStep) => void) =>
  streamSteps<Node, NodeStep>(
    "/api/nodes",
    input,
    (raw) => {
      const { node, ...rest } = raw as StreamEvent<never, NodeStep> & { node?: Node }
      return { ...rest, result: node }
    },
    onStep,
  )

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
  notifications: () => request<NotificationSettings>("/api/settings/notifications"),
  setNotifications: (input: NotificationInput) =>
    request<NotificationSettings>("/api/settings/notifications", {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  testNotifications: (input: NotificationInput) =>
    post<void>("/api/settings/notifications/test", input),
  backupSettings: () => request<BackupSettings>("/api/settings/backups"),
  setBackupSettings: (input: BackupInput) =>
    request<BackupSettings>("/api/settings/backups", { method: "PUT", body: JSON.stringify(input) }),
  disconnectBackups: () => request<BackupSettings>("/api/settings/backups", { method: "DELETE" }),
  testBackupSettings: (input: BackupInput) => post<void>("/api/settings/backups/test", input),
  backups: () => request<BackupObject[]>("/api/backups"),
  createBackup: () => post<BackupObject>("/api/backups"),
  deleteBackup: (name: string) => del(backupFileUrl(name)),
  setBackupEncryption: (passphrase: string) =>
    request<BackupSettings>("/api/settings/backups/encryption", {
      method: "PUT",
      body: JSON.stringify({ passphrase }),
    }),
  // Without a passphrase the panel tries its own.
  restoreBackup: (name: string, passphrase?: string) =>
    post<RestoreResult>(`${backupFileUrl(name)}/restore`, passphrase ? { passphrase } : undefined),
  importBackup: (file: File, passphrase?: string) =>
    request<RestoreResult>(`/api/backups/import?name=${encodeURIComponent(file.name)}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/octet-stream",
        // Headers carry only ASCII, and a passphrase may not be.
        ...(passphrase ? { "X-Backup-Passphrase": encodeURIComponent(passphrase) } : {}),
      },
      body: file,
    }),
  httpsUrl: () => request<{ url?: string }>("/api/https").then((r) => r.url ?? ""),

  dockerStatus: () => request<DockerStatus>("/api/system/docker"),
  updateStatus: () => request<UpdateStatus>("/api/system/update"),
  checkUpdate: () => post<UpdateStatus>("/api/system/update/check"),
  startUpdate: () => post<UpdateStatus>("/api/system/update"),
  events: (opts: { limit: number; category?: EventCategory }) =>
    request<ActivityEvent[]>(
      `/api/events?limit=${opts.limit}${opts.category ? `&category=${opts.category}` : ""}`,
    ),

  nodes: () => request<Node[]>("/api/nodes"),
  panelKey: () => request<PanelKey>("/api/nodes/key"),
  scanNode: (host: string, port: number) => post<HostKeyScan>("/api/nodes/scan", { host, port }),
  addNode,
  renameNode: (id: number, name: string) => patch<Node>(`/api/nodes/${id}`, { name }),
  // force forgets a node that is offline, leaving its servers running there.
  deleteNode: (id: number, force = false) => del(`/api/nodes/${id}${force ? "?force=true" : ""}`),

  instances: () => request<Instance[]>("/api/instances"),
  instanceDefaults: (nodeId = 0) =>
    request<InstanceSettings>(`/api/instances/defaults${nodeId ? `?node_id=${nodeId}` : ""}`),
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
  peerUsage: (instanceId: number, peerId: number) =>
    request<PeerUsage>(`${peerPath(instanceId, peerId)}/usage`),
}
