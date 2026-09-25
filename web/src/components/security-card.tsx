import { useEffect, useState } from "react"
import type { FormEvent, ReactNode } from "react"
import { Loader2, Lock, LockOpen, ShieldCheck, ShieldOff } from "lucide-react"
import { QRCodeSVG } from "qrcode.react"
import { Link } from "react-router-dom"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
import { CopyButton } from "@/components/copy-button"
import { FormField } from "@/components/form-field"
import { CodeInput } from "@/components/login-form"
import { ApiError, api } from "@/lib/api"
import type { TOTPSetup, User } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { errorMessage, execCommand } from "@/lib/format"
import { cn } from "@/lib/utils"

export function SecurityCard({ httpsUrl }: { httpsUrl: string }) {
  const { user, setUser } = useAuth()
  const [dialog, setDialog] = useState<"enable" | "disable">()
  const https = window.location.protocol === "https:"
  const totp = Boolean(user?.totp_enabled)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connection and sign-in</CardTitle>
        <CardDescription>How this panel is reached and how you sign in to it.</CardDescription>
      </CardHeader>
      <CardContent className="divide-y">
        <Row
          icon={https ? Lock : LockOpen}
          good={https}
          title={https ? "Encrypted connection" : "Unencrypted connection"}
        >
          {https ? (
            "Your password and session travel over HTTPS."
          ) : httpsUrl ? (
            <>
              HTTPS is ready at{" "}
              <a className="text-foreground font-medium underline" href={httpsUrl}>
                {httpsUrl.replace("https://", "")}
              </a>
              . Use that address instead of this one.
            </>
          ) : (
            <>
              Your password travels in plain text.{" "}
              <Link className="text-foreground font-medium underline" to="/settings/domain">
                Add a domain
              </Link>{" "}
              to get a free certificate.
            </>
          )}
        </Row>
        <Row
          icon={totp ? ShieldCheck : ShieldOff}
          good={totp}
          title={totp ? "Two-factor authentication is on" : "Two-factor authentication is off"}
          action={
            <Button
              variant={totp ? "outline" : "default"}
              size="sm"
              onClick={() => setDialog(totp ? "disable" : "enable")}
            >
              {totp ? "Turn off" : "Turn on"}
            </Button>
          }
        >
          {totp
            ? "Signing in asks for a code from your authenticator app."
            : "Ask for a code from an authenticator app too, so a leaked password is not enough."}
        </Row>
      </CardContent>

      <EnableDialog
        open={dialog === "enable"}
        onOpenChange={(open) => setDialog(open ? "enable" : undefined)}
        onDone={(u) => {
          setUser(u)
          setDialog(undefined)
          toast.success("Two-factor authentication is on. Other devices have been signed out.")
        }}
      />
      <DisableDialog
        open={dialog === "disable"}
        onOpenChange={(open) => setDialog(open ? "disable" : undefined)}
        onDone={(u) => {
          setUser(u)
          setDialog(undefined)
          toast.success("Two-factor authentication is off.")
        }}
      />
    </Card>
  )
}

function Row({ icon: Icon, good, title, action, children }: {
  icon: typeof Lock
  good: boolean
  title: string
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex items-start gap-3 py-4 first:pt-0 last:pb-0">
      <div
        className={cn(
          "flex size-9 shrink-0 items-center justify-center rounded-lg",
          good
            ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
            : "bg-orange-500/10 text-orange-700 dark:text-orange-400",
        )}
      >
        <Icon className="size-4" />
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-sm font-medium">{title}</p>
        <p className="text-muted-foreground text-sm">{children}</p>
      </div>
      {action}
    </div>
  )
}

interface DialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onDone: (user: User) => void
}

interface FormProps {
  busy: boolean
  setBusy: (busy: boolean) => void
  onDone: (user: User) => void
  onCancel: () => void
}

function EnableDialog({ open, onOpenChange, onDone }: DialogProps) {
  const [busy, setBusy] = useState(false)
  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Turn on two-factor authentication</DialogTitle>
          <DialogDescription>
            Scan the code with an authenticator app such as Google Authenticator, 1Password or
            Aegis, then enter the 6-digit code it shows.
          </DialogDescription>
        </DialogHeader>
        <EnableForm busy={busy} setBusy={setBusy} onDone={onDone} onCancel={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function EnableForm({ busy, setBusy, onDone, onCancel }: FormProps) {
  const [setup, setSetup] = useState<TOTPSetup>()
  const [loadError, setLoadError] = useState("")
  const [code, setCode] = useState("")
  const [password, setPassword] = useState("")
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  useEffect(() => {
    api.totpSetup().then(setSetup, (err: unknown) => setLoadError(errorMessage(err)))
  }, [])

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    if (!setup) return
    setFieldErrors({})
    setBusy(true)
    try {
      onDone(await api.totpEnable({ secret: setup.secret, code, password }))
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
      else toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  if (loadError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{loadError}</AlertDescription>
      </Alert>
    )
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <div className="flex flex-col items-center gap-3">
        {setup ? (
          <div className="rounded-lg bg-white p-3">
            <QRCodeSVG value={setup.uri} size={184} marginSize={0} />
          </div>
        ) : (
          <Skeleton className="size-[208px] rounded-lg" />
        )}
        {setup && (
          <div className="flex items-center gap-1">
            <code className="bg-muted rounded px-2 py-1 font-mono text-xs break-all">
              {setup.secret.match(/.{1,4}/g)?.join(" ")}
            </code>
            <CopyButton value={setup.secret} label="Copy key" />
          </div>
        )}
      </div>
      <FormField id="totp-code" label="Code from the app" error={fieldErrors.code}>
        <CodeInput id="totp-code" value={code} onChange={setCode} invalid={Boolean(fieldErrors.code)} />
      </FormField>
      <PasswordField value={password} onChange={setPassword} error={fieldErrors.password} />
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy || !setup || code.length < 6 || !password}>
          {busy && <Loader2 className="animate-spin" />}
          Turn on
        </Button>
      </DialogFooter>
    </form>
  )
}

function DisableDialog({ open, onOpenChange, onDone }: DialogProps) {
  const [busy, setBusy] = useState(false)
  const [container, setContainer] = useState<string>()

  useEffect(() => {
    if (open) api.dockerStatus().then((d) => setContainer(d.container ?? "")).catch(() => setContainer(""))
  }, [open])
  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Turn off two-factor authentication</DialogTitle>
          <DialogDescription>
            Signing in will only need your password again. Lost your phone? Run{" "}
            <code className="bg-muted rounded px-1 font-mono text-xs">
              {execCommand(container, "admin disable-2fa")}
            </code>{" "}
            on the server.
          </DialogDescription>
        </DialogHeader>
        <DisableForm busy={busy} setBusy={setBusy} onDone={onDone} onCancel={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function DisableForm({ busy, setBusy, onDone, onCancel }: FormProps) {
  const [code, setCode] = useState("")
  const [password, setPassword] = useState("")
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFieldErrors({})
    setBusy(true)
    try {
      onDone(await api.totpDisable({ code, password }))
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
      else toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <FormField id="totp-off-code" label="Code from the app" error={fieldErrors.code}>
        <CodeInput
          id="totp-off-code"
          value={code}
          onChange={setCode}
          invalid={Boolean(fieldErrors.code)}
          autoFocus
        />
      </FormField>
      <PasswordField value={password} onChange={setPassword} error={fieldErrors.password} />
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" variant="destructive" disabled={busy || code.length < 6 || !password}>
          {busy && <Loader2 className="animate-spin" />}
          Turn off
        </Button>
      </DialogFooter>
    </form>
  )
}

function PasswordField({ value, onChange, error }: {
  value: string
  onChange: (value: string) => void
  error?: string
}) {
  return (
    <FormField id="totp-password" label="Current password" error={error}>
      <Input
        id="totp-password"
        type="password"
        autoComplete="current-password"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-invalid={Boolean(error)}
      />
    </FormField>
  )
}
