import { useState } from "react"
import {
  History,
  Pencil,
  Plus,
  Send,
  Trash2,
  Webhook as WebhookIcon,
} from "lucide-react"
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
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { DeliveriesDialog } from "@/features/settings/webhook-deliveries-dialog"
import { allEvents, WebhookDialog } from "@/features/settings/webhook-dialog"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/api"
import type { Webhook } from "@/api"
import { errorMessage } from "@/lib/format"

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
