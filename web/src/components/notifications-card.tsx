import { useState } from "react"
import type { FormEvent, ReactNode } from "react"
import { Loader2, Send } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { FormField } from "@/components/form-field"
import { useNow } from "@/hooks/use-now"
import { ApiError, api } from "@/lib/api"
import type { NotificationGroup, NotificationInput, NotificationSettings, SMTPSecurity } from "@/lib/api"
import { errorMessage, formatRelative } from "@/lib/format"

const securities: { value: SMTPSecurity; label: string; port: number }[] = [
  { value: "starttls", label: "STARTTLS", port: 587 },
  { value: "tls", label: "TLS", port: 465 },
  { value: "none", label: "None", port: 25 },
]

const groups: { id: NotificationGroup; label: string; hint: string }[] = [
  { id: "servers", label: "Servers", hint: "Created, deleted, failed to deploy, went down or came back; nodes added, removed or offline." },
  { id: "devices", label: "Devices", hint: "Added, removed, enabled or disabled." },
  { id: "limits", label: "Data limits and access", hint: "A device used up its data, its access ended, its usage was reset, or it may connect again." },
  { id: "failed_logins", label: "Failed sign-ins", hint: "Someone tried a wrong password or code." },
  { id: "security", label: "Security changes", hint: "Password, two-factor, domain, email or backup settings changed, an API key or webhook was added or removed, or a backup was downloaded or restored." },
  { id: "backups", label: "Backup failures", hint: "A scheduled backup to S3 could not be made." },
  { id: "logins", label: "Successful sign-ins", hint: "Every time someone signs in to the panel." },
  { id: "connections", label: "Connections", hint: "Each time a device connects or disconnects. Can be many emails a day." },
]

type Form = Omit<NotificationInput, "to" | "port"> & { to: string; port: string }

function formOf(s: NotificationSettings): Form {
  return {
    enabled: s.enabled,
    host: s.host,
    port: String(s.port),
    security: s.security,
    username: s.username,
    password: "",
    from: s.from,
    to: s.to.join(", "),
    events: s.events,
  }
}

// Toggling a switch off and on again changes the order, not the settings.
const fingerprint = (f: Form) => JSON.stringify({ ...inputOf(f), events: [...f.events].sort() })

function inputOf(f: Form): NotificationInput {
  return {
    ...f,
    port: Number(f.port) || 0,
    to: f.to.split(/[\s,;]+/).filter(Boolean),
    password: f.password ? f.password : undefined,
  }
}

export function NotificationsCard({ settings, onSaved }: {
  settings: NotificationSettings
  onSaved: () => void
}) {
  const [form, setForm] = useState(() => formOf(settings))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState<"save" | "test">()
  const set = <K extends keyof Form>(key: K, value: Form[K]) => setForm((f) => ({ ...f, [key]: value }))
  const [saved, setSaved] = useState(() => formOf(settings))
  const dirty = fingerprint(form) !== fingerprint(saved)

  async function run(action: "save" | "test") {
    setErrors({})
    setBusy(action)
    try {
      if (action === "test") {
        await api.testNotifications(inputOf(form))
        toast.success(`Test email sent to ${form.to}. Check the inbox, and the spam folder.`)
      } else {
        const next = formOf(await api.setNotifications(inputOf(form)))
        setForm(next)
        setSaved(next)
        toast.success("Notification settings saved.")
      }
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setErrors(err.fields)
      else toast.error(errorMessage(err))
    } finally {
      setBusy(undefined)
      onSaved()
    }
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    void run("save")
  }

  function chooseSecurity(value: SMTPSecurity) {
    // Follow the port along only while it is still a standard one.
    const standard = securities.some((s) => String(s.port) === form.port)
    setForm((f) => ({
      ...f,
      security: value,
      port: standard ? String(securities.find((s) => s.value === value)!.port) : f.port,
    }))
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="grid items-start gap-6 xl:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>Email notifications</CardTitle>
          <CardDescription>
            Get an email when something happens on your VPN. Events close together arrive as one
            email.
          </CardDescription>
          <CardAction>
            <Switch
              checked={form.enabled}
              onCheckedChange={(enabled) => set("enabled", enabled)}
              aria-label="Send email notifications"
            />
          </CardAction>
        </CardHeader>
        <CardContent>
          <Section title="Send an email for">
            {errors.events && <p className="text-destructive text-sm">{errors.events}</p>}
            <ul className="divide-y rounded-lg border">
              {groups.map((g) => {
                const on = form.events.includes(g.id)
                return (
                  <li key={g.id} className="flex items-center justify-between gap-4 px-3 py-2.5">
                    <label htmlFor={`notify-${g.id}`} className="min-w-0 cursor-pointer">
                      <span className="block text-sm font-medium">{g.label}</span>
                      <span className="text-muted-foreground block text-xs">{g.hint}</span>
                    </label>
                    <Switch
                      id={`notify-${g.id}`}
                      checked={on}
                      onCheckedChange={(checked) =>
                        set("events", checked ? [...form.events, g.id] : form.events.filter((e) => e !== g.id))
                      }
                    />
                  </li>
                )
              })}
            </ul>
          </Section>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Mail server</CardTitle>
          <CardDescription>Works with any mail provider that offers SMTP.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <Section title="Server">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_7rem]">
              <FormField id="smtp-host" label="SMTP server" error={errors.host}>
                <Input
                  id="smtp-host"
                  placeholder="smtp.example.com"
                  value={form.host}
                  onChange={(e) => set("host", e.target.value)}
                  aria-invalid={Boolean(errors.host)}
                  autoComplete="off"
                />
              </FormField>
              <FormField id="smtp-port" label="Port" error={errors.port}>
                <Input
                  id="smtp-port"
                  inputMode="numeric"
                  value={form.port}
                  onChange={(e) => set("port", e.target.value.replace(/\D/g, ""))}
                  aria-invalid={Boolean(errors.port)}
                />
              </FormField>
            </div>
            <FormField
              id="smtp-security"
              label="Encryption"
              error={errors.security}
              hint="Most providers use STARTTLS on port 587 or TLS on port 465."
            >
              <div id="smtp-security" role="radiogroup" className="flex flex-wrap gap-1.5">
                {securities.map((s) => (
                  <Button
                    key={s.value}
                    type="button"
                    size="sm"
                    role="radio"
                    aria-checked={form.security === s.value}
                    variant={form.security === s.value ? "default" : "outline"}
                    onClick={() => chooseSecurity(s.value)}
                  >
                    {s.label}
                  </Button>
                ))}
              </div>
            </FormField>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="smtp-username" label="Username" hint="Usually the full email address.">
                <Input
                  id="smtp-username"
                  value={form.username}
                  onChange={(e) => set("username", e.target.value)}
                  autoComplete="off"
                />
              </FormField>
              <FormField
                id="smtp-password"
                label="Password"
                hint={settings.password_set ? "Saved. Leave empty to keep it." : "Some providers call this an app password."}
              >
                <Input
                  id="smtp-password"
                  type="password"
                  placeholder={settings.password_set ? "••••••••" : ""}
                  value={form.password}
                  onChange={(e) => set("password", e.target.value)}
                  autoComplete="new-password"
                />
              </FormField>
            </div>
          </Section>

          <Section title="Addresses">
            <FormField
              id="smtp-from"
              label="From"
              error={errors.from}
              hint="An address your provider lets you send as, for example tunploy@yourdomain.com."
            >
              <Input
                id="smtp-from"
                placeholder="tunploy@example.com"
                value={form.from}
                onChange={(e) => set("from", e.target.value)}
                aria-invalid={Boolean(errors.from)}
              />
            </FormField>
            <FormField id="smtp-to" label="Send to" error={errors.to} hint="One or more addresses, comma separated.">
              <Input
                id="smtp-to"
                placeholder="you@example.com"
                value={form.to}
                onChange={(e) => set("to", e.target.value)}
                aria-invalid={Boolean(errors.to)}
              />
            </FormField>
          </Section>

          <DeliveryStatus status={settings.status} />
        </CardContent>
      </Card>

      <div className="bg-background/80 supports-backdrop-filter:bg-background/60 sticky bottom-0 z-10 -mx-1 flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-3 shadow-sm backdrop-blur xl:col-span-2">
        <p className="text-muted-foreground text-sm">
          {dirty ? (
            <span className="font-medium text-amber-700 dark:text-amber-400">Unsaved changes</span>
          ) : (
            "Saved."
          )}{" "}
          Save stores both the events and the mail server.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" disabled={busy !== undefined} onClick={() => void run("test")}>
            {busy === "test" ? <Loader2 className="animate-spin" /> : <Send />}
            Send test email
          </Button>
          <Button type="submit" disabled={busy !== undefined || !dirty}>
            {busy === "save" && <Loader2 className="animate-spin" />}
            Save changes
          </Button>
        </div>
      </div>
    </form>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="space-y-4">
      <legend className="text-muted-foreground mb-3 text-xs font-medium tracking-wide uppercase">{title}</legend>
      {children}
    </fieldset>
  )
}

function DeliveryStatus({ status }: { status: NotificationSettings["status"] }) {
  const now = useNow()
  if (status.last_error && status.last_error_at) {
    return (
      <p className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-400">
        The last email failed {formatRelative(status.last_error_at, now)}: {status.last_error}
      </p>
    )
  }
  if (!status.last_sent_at) return null
  return (
    <p className="text-muted-foreground text-xs">
      Last email sent {formatRelative(status.last_sent_at, now)}.
    </p>
  )
}
