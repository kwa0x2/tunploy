// The panel's HTTP API, one module per resource like the Go packages behind it.
import { apiKeysApi } from "./api-keys"
import { authApi } from "./auth"
import { backupsApi } from "./backups"
import { eventsApi } from "./events"
import { instancesApi } from "./instances"
import { nodesApi } from "./nodes"
import { peersApi } from "./peers"
import { settingsApi } from "./settings"
import { systemApi } from "./system"
import { webhooksApi } from "./webhooks"

export const api = {
  ...authApi,
  ...settingsApi,
  ...backupsApi,
  ...systemApi,
  ...apiKeysApi,
  ...webhooksApi,
  ...eventsApi,
  ...nodesApi,
  ...instancesApi,
  ...peersApi,
}

export { ApiError, DeployError } from "./client"
export type { ApiErrorBody, Page } from "./client"
export * from "./api-keys"
export * from "./auth"
export * from "./backups"
export * from "./events"
export * from "./instances"
export * from "./nodes"
export * from "./peers"
export * from "./settings"
export * from "./system"
export * from "./webhooks"
