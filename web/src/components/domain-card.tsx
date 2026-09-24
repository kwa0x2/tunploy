import { useEffect, useState } from "react"
import type { FormEvent, ReactNode } from "react"
import { CircleAlert, CircleCheck, ExternalLink, Globe, Loader2, RotateCw } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/lib/api"
import type { DomainStatus } from "@/lib/api"
import { errorMessage } from "@/lib/format"

interface Props {
  status: DomainStatus
  serverAddress: string
  httpsUrl: string
  onChange: () => void
}

export function DomainCard({ status, serverAddress, httpsUrl, onChange }: Props) {
  const [domain, setDomain] = useState(status.domain)
  const [email, setEmail] = useState(status.email)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState(false)

  // Issuance that outlived the request finishes in the background.
  useEffect(() => {
    if (status.state !== "pending" || busy) return
    const timer = setInterval(onChange, 3000)
    return () => clearInterval(timer)
  }, [status.state, busy, onChange])

  async function save(event: FormEvent) {
    event.preventDefault()
    setFieldErrors({})
    setBusy(true)
    try {
      const saved = await api.setDomain({ domain: domain.trim(), email: email.trim() })
      setDomain(saved.domain)
      if (saved.state === "ready") toast.success(`HTTPS is ready at ${saved.domain}.`)
      else if (saved.state === "failed") toast.error("Could not get a certificate. See the details below.")
      onChange()
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
      else toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  async function retry() {
    setBusy(true)
    try {
      await api.retryDomain()
      onChange()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    await api.setDomain({ domain: "", email: "" })
    setDomain("")
    setEmail("")
    toast.success("Domain removed. The panel is back on plain HTTP only.")
    onChange()
  }

  const onHttps = window.location.protocol === "https:"
  const target = serverAddress || "this server's public IP"

  return (
    <Card>
      <form onSubmit={save} noValidate className="contents">
        <CardHeader>
          <CardTitle>Domain</CardTitle>
          <CardDescription>
            Reach this panel at your own domain over HTTPS. The certificate comes from Let's
            Encrypt for free and renews itself.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {!status.enabled && (
            <Alert variant="destructive">
              <AlertDescription>HTTPS is unavailable: {status.error}</AlertDescription>
            </Alert>
          )}

          {status.enabled && status.domain && (
            <StatusLine status={status} busy={busy} onRetry={() => void retry()}>
              {status.state === "ready" && !onHttps && httpsUrl && (
                <Button
                  size="sm"
                  variant="outline"
                  nativeButton={false}
                  render={<a href={httpsUrl} />}
                >
                  <ExternalLink />
                  Open {status.domain}
                </Button>
              )}
            </StatusLine>
          )}

          <div className="bg-muted/50 text-muted-foreground flex gap-3 rounded-lg border p-3 text-sm">
            <Globe className="mt-0.5 size-4 shrink-0" />
            <p>
              First add an <b className="text-foreground font-medium">A record</b> for the domain
              pointing to <code className="text-foreground font-mono text-xs">{target}</code>, and
              allow <b className="text-foreground font-medium">TCP 80 and 443</b> in your
              provider's firewall.
            </p>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <FormField id="panel_domain" label="Domain" error={fieldErrors.domain}>
              <Input
                id="panel_domain"
                placeholder="panel.example.com"
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                aria-invalid={Boolean(fieldErrors.domain)}
                disabled={!status.enabled}
              />
            </FormField>
            <FormField
              id="acme_email"
              label="Email (optional)"
              error={fieldErrors.email}
              hint="Let's Encrypt writes here if renewal ever fails."
            >
              <Input
                id="acme_email"
                type="email"
                placeholder="you@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                aria-invalid={Boolean(fieldErrors.email)}
                disabled={!status.enabled}
              />
            </FormField>
          </div>
        </CardContent>
        <CardFooter className="justify-end gap-2">
          {status.domain && (
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => setConfirmRemove(true)}
            >
              Remove
            </Button>
          )}
          <Button type="submit" disabled={busy || !status.enabled || !domain.trim()}>
            {busy && <Loader2 className="animate-spin" />}
            {busy ? "Requesting certificate…" : "Save"}
          </Button>
        </CardFooter>
      </form>

      <ConfirmDialog
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title="Remove the panel domain?"
        description={
          onHttps
            ? "This page is open over that domain, so it stops loading. Reach the panel on its IP and port afterwards."
            : "The panel goes back to plain HTTP only."
        }
        confirmLabel="Remove"
        onConfirm={remove}
      />
    </Card>
  )
}

function StatusLine({ status, busy, onRetry, children }: {
  status: DomainStatus
  busy: boolean
  onRetry: () => void
  children?: ReactNode
}) {
  if (status.state === "pending") {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-sm">
        <Loader2 className="size-4 animate-spin text-amber-600 dark:text-amber-400" />
        Requesting a certificate for <b className="font-medium">{status.domain}</b>…
      </div>
    )
  }
  if (status.state === "failed") {
    return (
      <div className="space-y-2 rounded-lg border border-red-500/30 bg-red-500/5 p-3 text-sm">
        <div className="flex items-center gap-2">
          <CircleAlert className="size-4 shrink-0 text-red-600 dark:text-red-400" />
          <span className="flex-1">
            No certificate for <b className="font-medium">{status.domain}</b> yet.
          </span>
          <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onRetry}>
            <RotateCw className={busy ? "animate-spin" : undefined} />
            Try again
          </Button>
        </div>
        {status.error && (
          <p className="text-muted-foreground font-mono text-xs break-words">{status.error}</p>
        )}
        <p className="text-muted-foreground text-xs">
          Usually the A record doesn't point here yet, or TCP 80 is blocked. Let's Encrypt allows
          only a few failed attempts an hour, so fix the cause before trying again.
        </p>
      </div>
    )
  }
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-3 text-sm">
      <CircleCheck className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
      <span className="flex-1">
        HTTPS is active for <b className="font-medium">{status.domain}</b>
        {status.expires && (
          <span className="text-muted-foreground">
            {" "}
            · certificate valid until {new Date(status.expires).toLocaleDateString()}
          </span>
        )}
      </span>
      {children}
    </div>
  )
}
