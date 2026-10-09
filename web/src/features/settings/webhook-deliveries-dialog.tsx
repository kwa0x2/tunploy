import { useCallback, useState } from "react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/api"
import type { DeliveryState, Webhook, WebhookDelivery } from "@/api"
import { errorMessage, formatDateTime, formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"

const stateStyle: Record<DeliveryState, string> = {
  pending: "bg-amber-500/10 text-amber-700 dark:text-amber-400",
  succeeded: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  failed: "bg-red-500/10 text-red-600 dark:text-red-400",
}

export function DeliveriesDialog({ hook, onOpenChange }: { hook?: Webhook; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={hook !== undefined} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Deliveries</DialogTitle>
          <DialogDescription className="truncate font-mono text-xs">{hook?.url}</DialogDescription>
        </DialogHeader>
        {hook && <DeliveryList key={hook.id} hook={hook} />}
      </DialogContent>
    </Dialog>
  )
}

function DeliveryList({ hook }: { hook: Webhook }) {
  const [older, setOlder] = useState<WebhookDelivery[]>([])
  const [more, setMore] = useState<boolean>()
  const { data, error, reload } = useResource(
    useCallback(() => api.webhookDeliveries(hook.id), [hook.id]),
    5_000,
  )
  const now = useNow()
  const list = [...(data?.data ?? []), ...older.filter((d) => !data?.data.some((x) => x.id === d.id))]
  const hasMore = more ?? data?.has_more

  async function loadMore() {
    const last = list[list.length - 1]
    try {
      const pg = await api.webhookDeliveries(hook.id, last.id)
      setOlder((o) => [...o, ...pg.data])
      setMore(pg.has_more)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  async function retry(d: WebhookDelivery) {
    try {
      await api.retryDelivery(hook.id, d.id)
      toast.success("Sending again")
      reload()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  if (error !== undefined && !data) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{errorMessage(error)}</AlertDescription>
      </Alert>
    )
  }
  if (!data) return <Skeleton className="h-48 rounded-lg" />
  if (list.length === 0) {
    return (
      <p className="text-muted-foreground rounded-lg border border-dashed py-8 text-center text-sm">
        Nothing sent yet. Use Send test, or wait for an event.
      </p>
    )
  }

  return (
    <div className="max-h-[60vh] min-w-0 space-y-2 overflow-y-auto">
      <ul className="divide-y rounded-lg border">
        {list.map((d) => (
          <li key={d.id} className="min-w-0 space-y-1 px-3 py-2.5">
            <div className="flex items-center justify-between gap-2">
              <p className="flex min-w-0 items-center gap-2 text-sm">
                <span className={cn("shrink-0 rounded px-1.5 py-0.5 text-xs", stateStyle[d.state])}>{d.state}</span>
                <code className="truncate font-mono text-xs">{d.kind}</code>
              </p>
              <Button size="xs" variant="outline" disabled={!hook.enabled} onClick={() => retry(d)}>
                {d.state === "succeeded" ? "Send again" : "Retry now"}
              </Button>
            </div>
            <p className="text-muted-foreground text-xs">
              {formatRelative(d.created_at, now)}
              {d.attempts > 0 && ` · ${d.attempts} ${d.attempts === 1 ? "try" : "tries"}`}
              {d.response_status ? ` · HTTP ${d.response_status}` : ""}
              {d.duration_ms ? ` · ${d.duration_ms} ms` : ""}
              {d.state === "pending" && d.next_attempt_at && d.attempts > 0 &&
                ` · next try ${formatDateTime(d.next_attempt_at)}`}
            </p>
            {d.error && <p className="text-xs break-words text-red-600 dark:text-red-400">{d.error}</p>}
            <details className="text-xs">
              <summary className="text-muted-foreground cursor-pointer select-none">Payload</summary>
              <pre className="bg-muted mt-1 overflow-x-auto rounded-md p-2 font-mono">
                {JSON.stringify(d.payload, null, 2)}
              </pre>
            </details>
          </li>
        ))}
      </ul>
      {hasMore && (
        <Button variant="outline" size="sm" className="w-full" onClick={loadMore}>
          Load older
        </Button>
      )}
    </div>
  )
}
