import { Check, Circle, Loader2, X } from "lucide-react"
import { ApiError, DeployError } from "@/lib/api"
import type { ProvisionStep } from "@/lib/api"
import { cn } from "@/lib/utils"

function stepsFor(port?: number): { step: ProvisionStep; label: string }[] {
  return [
    { step: "image", label: "WireGuard image ready" },
    { step: "container", label: port ? `Container started (UDP ${port})` : "Container started" },
    { step: "interface", label: "Interface wg0 up" },
    { step: "firewall", label: "Firewall configured" },
    { step: "nat", label: "NAT configured" },
  ]
}

export type DeployState =
  | { status: "running"; done: ProvisionStep[] }
  | { status: "ready"; done: ProvisionStep[] }
  | { status: "failed"; done: ProvisionStep[]; error: unknown }

export function DeployProgress({ state, port }: { state: DeployState; port?: number }) {
  const steps = stepsFor(port)
  const current = steps.find((s) => !state.done.includes(s.step))?.step

  return (
    <div className="space-y-4">
      <ol className="space-y-2.5 font-mono text-sm">
        {steps.map(({ step, label }) => {
          const done = state.done.includes(step)
          const active = step === current
          const failed = active && state.status === "failed"
          return (
            <li
              key={step}
              className={cn(
                "flex items-center gap-2.5",
                !done && !active && "text-muted-foreground/60",
                failed && "text-destructive",
              )}
            >
              {done ? (
                <Check className="size-4 text-emerald-600 dark:text-emerald-400" />
              ) : failed ? (
                <X className="size-4" />
              ) : active ? (
                <Loader2 className="text-primary size-4 animate-spin" />
              ) : (
                <Circle className="size-4" />
              )}
              {label}
            </li>
          )
        })}
      </ol>

      {state.status === "running" && current === "image" && (
        <p className="text-muted-foreground text-xs">
          The first deploy builds the WireGuard image, which takes a few seconds.
        </p>
      )}
      {state.status === "ready" && (
        <p className="font-mono text-sm font-medium text-emerald-700 dark:text-emerald-400">
          VPN is ready.
        </p>
      )}
      {state.status === "failed" && <ErrorConsole error={state.error} />}
    </div>
  )
}

// Stays dark in both themes so it reads as terminal output.
function ErrorConsole({ error }: { error: unknown }) {
  const code = error instanceof ApiError ? error.code : "unknown_error"
  const message = error instanceof ApiError ? error.message : "Something went wrong."
  const log = error instanceof DeployError ? error.log : []

  return (
    <div className="overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950 text-zinc-100 shadow-sm">
      <div className="flex items-center gap-2 border-b border-zinc-800 px-3 py-2 text-xs">
        <span className="flex gap-1.5" aria-hidden>
          <span className="size-2.5 rounded-full bg-red-500/80" />
          <span className="size-2.5 rounded-full bg-zinc-700" />
          <span className="size-2.5 rounded-full bg-zinc-700" />
        </span>
        <span className="text-zinc-400">deploy</span>
        <span className="ml-auto rounded bg-red-500/15 px-1.5 py-0.5 font-mono text-red-400">{code}</span>
      </div>
      <pre className="max-h-64 overflow-auto p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">
        <span className="text-red-400">error: {message}</span>
        {log.length > 0 && (
          <>
            {"\n\n"}
            <span className="text-zinc-500"># container output</span>
            {"\n"}
            {log.join("\n")}
          </>
        )}
      </pre>
    </div>
  )
}
