import { del, patch, post, request, streamSteps } from "./client"
import type { StreamEvent } from "./client"

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

export const nodesApi = {
  nodes: () => request<Node[]>("/api/nodes"),
  panelKey: () => request<PanelKey>("/api/nodes/key"),
  scanNode: (host: string, port: number) => post<HostKeyScan>("/api/nodes/scan", { host, port }),
  addNode,
  renameNode: (id: number, name: string) => patch<Node>(`/api/nodes/${id}`, { name }),
  // force forgets a node that is offline, leaving its servers running there.
  deleteNode: (id: number, force = false) => del(`/api/nodes/${id}${force ? "?force=true" : ""}`),
}
