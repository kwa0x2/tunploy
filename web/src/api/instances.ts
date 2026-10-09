import { del, patch, post, request, streamSteps, streamText } from "./client"
import type { ApiErrorBody } from "./client"

export type InstanceState = "running" | "restarting" | "stopped" | "not_deployed" | "unknown"

export interface InstanceSettings {
  address: string
  listen_port: number
  endpoint: string
  dns: string[]
  // Clients ask a resolver on the server, which forwards to dns.
  dns_on_server?: boolean
  mtu: number
  persistent_keepalive: number
  client_allowed_ips: string[]
  // ISO 3166 code such as "DE"; for the defaults, a guess from the endpoint.
  country: string
  city?: string
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

export const instancePath = (id: number) => `/api/instances/${id}`

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

export const instancesApi = {
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
}
