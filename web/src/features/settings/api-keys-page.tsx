import { useState } from "react"
import { KeySquare, Plus, Trash2, TriangleAlert } from "lucide-react"
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
import { ConfirmDialog } from "@/components/confirm-dialog"
import { CopyButton } from "@/components/copy-button"
import { PageHeader } from "@/components/page-header"
import { CreateKeyDialog } from "@/features/settings/create-api-key-dialog"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/api"
import type { ApiKey } from "@/api"
import { errorMessage, formatDateTime, formatRelative } from "@/lib/format"

export function ApiKeysSettingsPage() {
  const { data: keys, error, reload } = useResource(api.apiKeys)
  const [creating, setCreating] = useState(false)
  const [revoking, setRevoking] = useState<ApiKey>()
  const now = useNow()

  return (
    <>
      <PageHeader
        title="API keys"
        description="Let your own programs manage devices: a billing backend, a bot or a script."
      />
      <div className="grid max-w-6xl items-start gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Keys</CardTitle>
            <CardDescription>Each key only does what its scopes allow.</CardDescription>
            <CardAction>
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus />
                Create key
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {error !== undefined && !keys && (
              <Alert variant="destructive">
                <AlertDescription>{errorMessage(error)}</AlertDescription>
              </Alert>
            )}
            {!keys && error === undefined && <Skeleton className="h-24 rounded-lg" />}
            {keys && keys.length === 0 && (
              <div className="text-muted-foreground flex flex-col items-center gap-2 rounded-lg border border-dashed py-8 text-center text-sm">
                <KeySquare className="size-6" />
                No API keys yet.
              </div>
            )}
            {keys && keys.length > 0 && (
              <ul className="divide-y rounded-lg border">
                {keys.map((k) => (
                  <KeyRow key={k.id} apiKey={k} now={now} onRevoke={() => setRevoking(k)} />
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
        <UsageCard />
      </div>

      <CreateKeyDialog open={creating} onOpenChange={setCreating} onCreated={reload} />
      <ConfirmDialog
        open={revoking !== undefined}
        onOpenChange={(open) => !open && setRevoking(undefined)}
        title={`Revoke ${revoking?.name ?? "key"}?`}
        description="Programs using this key stop working right away. Devices it created stay."
        confirmLabel="Revoke"
        onConfirm={async () => {
          if (!revoking) return
          await api.deleteApiKey(revoking.id)
          toast.success(`Revoked ${revoking.name}`)
          reload()
        }}
      />
    </>
  )
}

function KeyRow({ apiKey: k, now, onRevoke }: { apiKey: ApiKey; now: number; onRevoke: () => void }) {
  const expired = k.expires_at !== undefined && new Date(k.expires_at).getTime() <= now
  return (
    <li className="flex items-start justify-between gap-4 px-3 py-3">
      <div className="min-w-0 space-y-1">
        <p className="flex flex-wrap items-center gap-2 text-sm font-medium">
          {k.name}
          <code className="bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-xs font-normal">
            {k.prefix}…
          </code>
          {expired && (
            <span className="rounded bg-red-500/10 px-1.5 py-0.5 text-xs font-normal text-red-600 dark:text-red-400">
              Expired
            </span>
          )}
        </p>
        <div className="flex flex-wrap gap-1">
          {k.scopes.map((s) => (
            <span key={s} className="bg-primary/10 text-foreground rounded px-1.5 py-0.5 font-mono text-[11px]">
              {s}
            </span>
          ))}
        </div>
        <p className="text-muted-foreground text-xs">
          {k.last_used_at
            ? `Last used ${formatRelative(k.last_used_at, now)}${k.last_used_ip ? ` from ${k.last_used_ip}` : ""}`
            : "Never used"}
          {" · "}
          {k.expires_at ? `${expired ? "Expired" : "Expires"} ${formatDateTime(k.expires_at)}` : "Never expires"}
        </p>
      </div>
      <Button variant="ghost" size="icon-sm" aria-label={`Revoke ${k.name}`} onClick={onRevoke}>
        <Trash2 />
      </Button>
    </li>
  )
}

function UsageCard() {
  const base = `${window.location.origin}/api/v1`
  const example = `curl ${base}/devices \\
  -H "Authorization: Bearer tp_..." \\
  -H "Idempotency-Key: order-1042" \\
  -H "Content-Type: application/json" \\
  -d '{"server_id": 1, "external_id": "user_123"}'`

  return (
    <Card>
      <CardHeader>
        <CardTitle>Using the API</CardTitle>
        <CardDescription>
          Send the key as a Bearer token. Replies are JSON, errors use the same shape as the panel.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <div className="space-y-1.5">
          <p className="font-medium">Base URL</p>
          <div className="flex items-center gap-2">
            <code className="bg-muted min-w-0 flex-1 truncate rounded-md px-3 py-2 font-mono text-xs">{base}</code>
            <CopyButton value={base} label="Copy base URL" />
          </div>
        </div>
        <div className="space-y-1.5">
          <p className="font-medium">Create a device</p>
          <pre className="bg-muted overflow-x-auto rounded-md p-3 font-mono text-xs leading-relaxed">{example}</pre>
          <p className="text-muted-foreground text-xs">
            The reply carries the device and its config. Send a <code>public_key</code> instead to
            keep the private key on the device. A retry with the same Idempotency-Key returns the
            first reply instead of making a second device.
          </p>
        </div>
        <p className="text-muted-foreground text-xs">
          Every endpoint is described in{" "}
          <a href="/api/v1/openapi.json" target="_blank" rel="noreferrer" className="text-foreground underline underline-offset-4">
            openapi.json
          </a>
          , ready for Postman, Insomnia or a client generator.
        </p>
        <Alert>
          <TriangleAlert />
          <AlertDescription>
            Keep keys on your server. A key inside a mobile app or web page can be pulled out by
            anyone who has it.
          </AlertDescription>
        </Alert>
      </CardContent>
    </Card>
  )
}
