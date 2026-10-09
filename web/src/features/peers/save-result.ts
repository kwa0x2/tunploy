import { ApiError } from "@/api"
import type { Peer } from "@/api"

export type SaveResult = { peer?: Peer; warning?: string }

// apply_failed means the change is saved but the running tunnel missed it;
// the next restart picks it up, so it is a warning, not a failure.
export function notApplied(err: unknown): string | undefined {
  return err instanceof ApiError && err.code === "apply_failed" ? err.message : undefined
}
