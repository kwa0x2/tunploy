import { useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { CopyButton } from "@/components/copy-button"
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/api"
import type { CreatedWebhook, Webhook } from "@/api"
import { errorMessage } from "@/lib/format"

const kinds: { family: string; kinds: string[] }[] = [
  {
    family: "Devices",
    kinds: [
      "device.created", "device.deleted", "device.renamed", "device.moved", "device.enabled", "device.disabled",
      "device.limits_changed", "device.limit_reached", "device.expired", "device.unblocked",
      "device.usage_reset", "device.shared", "device.unshared", "device.connected", "device.disconnected",
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

export const allEvents = "*"

export function WebhookDialog({ target, onOpenChange, onSaved }: {
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
