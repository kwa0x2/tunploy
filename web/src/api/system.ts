import { post, request } from "./client"

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

export const systemApi = {
  dockerStatus: () => request<DockerStatus>("/api/system/docker"),
  updateStatus: () => request<UpdateStatus>("/api/system/update"),
  checkUpdate: () => post<UpdateStatus>("/api/system/update/check"),
  startUpdate: () => post<UpdateStatus>("/api/system/update"),
}
