import { useCallback, useState } from "react"
import type { FormEvent } from "react"
import { History, Loader2, Pencil, Plus, Send, Trash2, Webhook as WebhookIcon } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { CopyButton } from "@/components/copy-button"
import { FormField } from "@/components/form-field"
import { PageHeader } from "@/components/page-header"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { ApiError, api } from "@/lib/api"
import type { CreatedWebhook, DeliveryState, Webhook, WebhookDelivery } from "@/lib/api"
import { errorMessage, formatDateTime, formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"

const kinds: { family: string; kinds: string[] }[] = [
  {
    family: "Devices",
    kinds: [
      "device.created", "device.deleted", "device.renamed", "device.enabled", "device.disabled",
      "device.limits_changed", "device.limit_reached", "device.expired", "device.unblocked",
      "device.usage_reset", "device.connected", "device.disconnected",
    ],
  },
  {
    family: "Servers",
    kinds: [
      "server.created", "server.deploy_failed", "server.updated", "server.deleted", "server.started",
      "server.stopped", "server.restarted", "server.down", "server.recovered",
    ],
  },
  { family: "Nodes", kinds: ["node.added", "node.renamed", "node.deleted", "node.offline", "node.online"] },
]

const allEvents = "*"

export function WebhooksSettingsPage() {
  const { data: hooks, error, reload } = useResource(api.webhooks)
  const [editing, setEditing] = useState<Webhook | "new">()
  const [deleting, setDeleting] = useState<Webhook>()
  const [viewing, setViewing] = useState<Webhook>()

  async function setEnabled(hook: Webhook, enabled: boolean) {
    try {
      await api.updateWebhook(hook.id, { enabled })
      reload()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  async function ping(hook: Webhook) {
    try {
      await api.pingWebhook(hook.id)
      toast.success("Test delivery queued")
      setViewing(hook)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <>
      <PageHeader
        title="Webhooks"
        description="Tell your own programs about device and server events the moment they happen."
      />
      <div className="grid max-w-6xl items-start gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Endpoints</CardTitle>
            <CardDescription>Each event is posted as JSON to every endpoint that wants it.</CardDescription>
            <CardAction>
              <Button size="sm" onClick={() => setEditing("new")}>
                <Plus />
                Add webhook
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {error !== undefined && !hooks && (
              <Alert variant="destructive">
                <AlertDescription>{errorMessage(error)}</AlertDescription>
              </Alert>
            )}
            {!hooks && error === undefined && <Skeleton className="h-24 rounded-lg" />}
            {hooks && hooks.length === 0 && (
              <div className="text-muted-foreground flex flex-col items-center gap-2 rounded-lg border border-dashed py-8 text-center text-sm">
                <WebhookIcon className="size-6" />
                No webhooks yet.
              </div>
            )}
            {hooks && hooks.length > 0 && (
              <ul className="divide-y rounded-lg border">
                {hooks.map((h) => (
                  <li key={h.id} className="flex items-start justify-between gap-3 px-3 py-3">
                    <div className="min-w-0 space-y-1">
                      <p className="truncate font-mono text-xs font-medium" title={h.url}>
                        {h.url}
                      </p>
                      {h.description && <p className="text-sm">{h.description}</p>}
                      <p className="text-muted-foreground text-xs">
                        {eventsText(h.events)}
                        {h.created_by?.startsWith("api:") && ` · added by API key ${h.created_by.slice(4)}`}
                      </p>
                      <div className="flex flex-wrap gap-1 pt-1">
                        <Button size="xs" variant="outline" onClick={() => setViewing(h)}>
                          <History />
                          Deliveries
                        </Button>
                        <Button size="xs" variant="outline" disabled={!h.enabled} onClick={() => ping(h)}>
                          <Send />
                          Send test
                        </Button>
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <Switch
                        checked={h.enabled}
                        aria-label={h.enabled ? "Turn off" : "Turn on"}
                        onCheckedChange={(on) => setEnabled(h, on)}
                      />
                      <Button variant="ghost" size="icon-sm" aria-label="Edit webhook" onClick={() => setEditing(h)}>
                        <Pencil />
                      </Button>
                      <Button variant="ghost" size="icon-sm" aria-label="Delete webhook" onClick={() => setDeleting(h)}>
                        <Trash2 />
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
        <SignatureCard />
      </div>

      <WebhookDialog
        target={editing}
        onOpenChange={(open) => !open && setEditing(undefined)}
        onSaved={reload}
      />
      <DeliveriesDialog hook={viewing} onOpenChange={(open) => !open && setViewing(undefined)} />
      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(open) => !open && setDeleting(undefined)}
        title="Delete this webhook?"
        description="Events stop going to it right away, and its delivery history goes too."
        confirmLabel="Delete"
        onConfirm={async () => {
          if (!deleting) return
          await api.deleteWebhook(deleting.id)
          toast.success("Webhook deleted")
          reload()
        }}
      />
    </>
  )
}

function eventsText(events: string[]) {
  if (events[0] === allEvents) return "All events"
  if (events.length <= 2) return events.join(", ")
  return `${events.length} events`
}

function SignatureCard() {
  const example = `import crypto from "node:crypto"

// header: the X-Tunploy-Signature value, body: the raw request body
function verify(secret, header, body) {
  const { t, v1 } = Object.fromEntries(header.split(",").map((p) => p.split("=")))
  if (Math.abs(Date.now() / 1000 - Number(t)) > 300) return false
  const want = crypto.createHmac("sha256", secret).update(\`\${t}.\${body}\`).digest("hex")
  return v1.length === want.length && crypto.timingSafeEqual(Buffer.from(v1), Buffer.from(want))
}`

  return (
    <Card>
      <CardHeader>
        <CardTitle>Receiving events</CardTitle>
        <CardDescription>
          The body has the same fields as an item from <code>GET /api/v1/events</code>.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <ul className="text-muted-foreground space-y-1 text-xs">
          <li>
            <code className="text-foreground">X-Tunploy-Event</code> the event kind, or <code>ping</code> for a test
          </li>
          <li>
            <code className="text-foreground">X-Tunploy-Delivery</code> the same on every retry, to skip repeats
          </li>
          <li>
            <code className="text-foreground">X-Tunploy-Signature</code> <code>t=…,v1=…</code>: HMAC-SHA256 of the
            timestamp, a dot and the body, keyed with the webhook's secret
          </li>
        </ul>
        <pre className="bg-muted overflow-x-auto rounded-md p-3 font-mono text-xs leading-relaxed">{example}</pre>
        <p className="text-muted-foreground text-xs">
          Answer with any 2xx status within 10 seconds. Anything else is tried again after 1 and 5
          minutes, then 30 minutes, 2, 6 and 12 hours. Redirects are not followed.
        </p>
      </CardContent>
    </Card>
  )
}

function WebhookDialog({ target, onOpenChange, onSaved }: {
  target?: Webhook | "new"
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<CreatedWebhook>()
  const open = target !== undefined

  function close() {
    onOpenChange(false)
    setCreated(undefined)
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && !next && close()}>
      <DialogContent className="sm:max-w-xl">
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Copy the signing secret</DialogTitle>
              <DialogDescription>
                Your endpoint checks signatures with it. This is the only time the panel shows it;
                if you lose it, delete the webhook and add it again.
              </DialogDescription>
            </DialogHeader>
            <div className="flex items-center gap-2">
              <code className="bg-muted min-w-0 flex-1 rounded-md px-3 py-2 font-mono text-xs break-all">
                {created.secret}
              </code>
              <CopyButton value={created.secret} label="Copy secret" />
            </div>
            <DialogFooter>
              <Button onClick={close}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>{target === "new" ? "Add webhook" : "Edit webhook"}</DialogTitle>
              <DialogDescription>Choose where events go and which ones.</DialogDescription>
            </DialogHeader>
            {open && (
              <WebhookForm
                key={target === "new" ? "new" : target.id}
                hook={target === "new" ? undefined : target}
                busy={busy}
                setBusy={setBusy}
                onCancel={close}
                onSaved={(hook) => {
                  onSaved()
                  if ("secret" in hook) setCreated(hook as CreatedWebhook)
                  else close()
                }}
              />
            )}
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

function WebhookForm({ hook, busy, setBusy, onCancel, onSaved }: {
  hook?: Webhook
  busy: boolean
  setBusy: (busy: boolean) => void
  onCancel: () => void
  onSaved: (hook: Webhook | CreatedWebhook) => void
}) {
  const [url, setUrl] = useState(hook?.url ?? "")
  const [description, setDescription] = useState(hook?.description ?? "")
  const [all, setAll] = useState(!hook || hook.events[0] === allEvents)
  const [chosen, setChosen] = useState<string[]>(
    hook && hook.events[0] !== allEvents ? hook.events : ["device.limit_reached", "device.expired"],
  )
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")

  function toggle(kind: string) {
    setChosen((c) => (c.includes(kind) ? c.filter((k) => k !== kind) : [...c, kind]))
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setErrors({})
    setFormError("")
    setBusy(true)
    const input = { url, description, events: all ? [allEvents] : chosen }
    try {
      onSaved(hook ? await api.updateWebhook(hook.id, input) : await api.createWebhook(input))
      toast.success(hook ? "Webhook saved" : "Webhook added")
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setErrors(err.fields)
      else setFormError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="space-y-4">
      {formError && (
        <Alert variant="destructive">
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      )}
      <FormField id="hook-url" label="URL" error={errors.url}>
        <Input
          id="hook-url"
          type="url"
          placeholder="https://example.com/hooks/tunploy"
          autoFocus
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          aria-invalid={Boolean(errors.url)}
        />
      </FormField>
      <FormField id="hook-description" label="Description" error={errors.description}>
        <Input
          id="hook-description"
          placeholder="Billing backend"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </FormField>
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-4">
          <label htmlFor="hook-all" className="cursor-pointer text-sm font-medium">
            All events
          </label>
          <Switch id="hook-all" checked={all} onCheckedChange={setAll} />
        </div>
        {errors.events && <p className="text-destructive text-sm">{errors.events}</p>}
        {!all && (
          <div className="max-h-72 space-y-3 overflow-y-auto rounded-lg border p-3">
            {kinds.map((group) => (
              <div key={group.family} className="space-y-1.5">
                <p className="text-muted-foreground text-xs font-medium">{group.family}</p>
                <div className="flex flex-wrap gap-1">
                  {group.kinds.map((kind) => (
                    <Button
                      key={kind}
                      type="button"
                      size="xs"
                      aria-pressed={chosen.includes(kind)}
                      variant={chosen.includes(kind) ? "secondary" : "outline"}
                      className="font-mono text-[11px]"
                      onClick={() => toggle(kind)}
                    >
                      {kind}
                    </Button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy || !url.trim() || (!all && chosen.length === 0)}>
          {busy && <Loader2 className="animate-spin" />}
          {hook ? "Save" : "Add webhook"}
        </Button>
      </DialogFooter>
    </form>
  )
}

const stateStyle: Record<DeliveryState, string> = {
  pending: "bg-amber-500/10 text-amber-700 dark:text-amber-400",
  succeeded: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  failed: "bg-red-500/10 text-red-600 dark:text-red-400",
}

function DeliveriesDialog({ hook, onOpenChange }: { hook?: Webhook; onOpenChange: (open: boolean) => void }) {
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
