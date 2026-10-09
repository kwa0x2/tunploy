import { useState } from "react"
import type { FormEvent, ReactNode } from "react"
import { Loader2, PlugZap } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/api"
import type { BackupInput, BackupSchedule, BackupSettings } from "@/api"
import { errorMessage } from "@/lib/format"

const schedules: { value: BackupSchedule; label: string }[] = [
  { value: "off", label: "Off" },
  { value: "daily", label: "Daily" },
  { value: "weekly", label: "Weekly, on Sundays" },
]

const hours = Array.from({ length: 24 }, (_, h) => h)

const pad = (n: number) => String(n).padStart(2, "0")

type Form = Omit<BackupInput, "keep" | "secret_key"> & { keep: string; secret_key: string }

function formOf(s: BackupSettings): Form {
  return {
    endpoint: s.endpoint,
    region: s.region,
    bucket: s.bucket,
    prefix: s.prefix,
    access_key: s.access_key,
    secret_key: "",
    path_style: s.path_style,
    schedule: s.schedule,
    hour: s.hour,
    keep: String(s.keep),
  }
}

function inputOf(f: Form): BackupInput {
  return { ...f, keep: Number(f.keep) || 0, secret_key: f.secret_key ? f.secret_key : undefined }
}

export function BackupStorageCard({ settings, onSaved }: { settings: BackupSettings; onSaved: () => void }) {
  const [form, setForm] = useState(() => formOf(settings))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState<"save" | "test">()
  const [confirmDisconnect, setConfirmDisconnect] = useState(false)
  const set = <K extends keyof Form>(key: K, value: Form[K]) => setForm((f) => ({ ...f, [key]: value }))

  async function run(action: "save" | "test") {
    setErrors({})
    setBusy(action)
    try {
      if (action === "test") {
        await api.testBackupSettings(inputOf(form))
        toast.success(`Connected to ${form.bucket}: Tunploy can write, list and delete files there.`)
      } else {
        const saved = await api.setBackupSettings(inputOf(form))
        setForm(formOf(saved))
        toast.success(settings.connected ? "Backup settings saved." : `Backups will be stored in ${saved.bucket}.`)
        onSaved()
      }
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setErrors(err.fields)
      else toast.error(errorMessage(err))
    } finally {
      setBusy(undefined)
    }
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    void run("save")
  }

  async function disconnect() {
    const saved = await api.disconnectBackups()
    setForm(formOf(saved))
    toast.success("Bucket disconnected. Backups already in it were left alone.")
    onSaved()
  }

  return (
    <Card>
      <form onSubmit={handleSubmit} noValidate className="contents">
        <CardHeader>
          <CardTitle>Backup storage</CardTitle>
          <CardDescription>
            Keep copies of the panel in an S3 bucket, off this server. Works with AWS S3, Cloudflare
            R2, Backblaze B2, MinIO and other S3-compatible storage.
          </CardDescription>
          {settings.connected && (
            <CardAction>
              <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2.5 py-1 text-xs font-medium text-emerald-700 dark:text-emerald-400">
                <PlugZap className="size-3.5" />
                Connected
              </span>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="space-y-6">
          <Section title="Bucket">
            <FormField
              id="s3-endpoint"
              label="Endpoint"
              error={errors.endpoint}
              hint={
                <>
                  Leave empty for AWS. R2: https://&lt;account&gt;.r2.cloudflarestorage.com · B2:
                  https://s3.&lt;region&gt;.backblazeb2.com · MinIO: http://host:9000
                </>
              }
            >
              <Input
                id="s3-endpoint"
                placeholder="https://s3.amazonaws.com"
                value={form.endpoint}
                onChange={(e) => set("endpoint", e.target.value)}
                aria-invalid={Boolean(errors.endpoint)}
                autoComplete="off"
              />
            </FormField>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="s3-bucket" label="Bucket" error={errors.bucket}>
                <Input
                  id="s3-bucket"
                  placeholder="my-backups"
                  value={form.bucket}
                  onChange={(e) => set("bucket", e.target.value)}
                  aria-invalid={Boolean(errors.bucket)}
                  autoComplete="off"
                />
              </FormField>
              <FormField
                id="s3-region"
                label="Region"
                error={errors.region}
                hint="us-east-1 if empty; auto for R2."
              >
                <Input
                  id="s3-region"
                  placeholder="us-east-1"
                  value={form.region}
                  onChange={(e) => set("region", e.target.value)}
                  aria-invalid={Boolean(errors.region)}
                  autoComplete="off"
                />
              </FormField>
            </div>
            <FormField
              id="s3-prefix"
              label="Folder"
              error={errors.prefix}
              hint="Optional. Lets several panels share one bucket."
            >
              <Input
                id="s3-prefix"
                placeholder="tunploy/"
                value={form.prefix}
                onChange={(e) => set("prefix", e.target.value)}
                aria-invalid={Boolean(errors.prefix)}
                autoComplete="off"
              />
            </FormField>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField id="s3-access-key" label="Access key" error={errors.access_key}>
                <Input
                  id="s3-access-key"
                  value={form.access_key}
                  onChange={(e) => set("access_key", e.target.value)}
                  aria-invalid={Boolean(errors.access_key)}
                  autoComplete="off"
                />
              </FormField>
              <FormField
                id="s3-secret-key"
                label="Secret key"
                error={errors.secret_key}
                hint={settings.secret_key_set ? "Saved. Leave empty to keep it." : undefined}
              >
                <Input
                  id="s3-secret-key"
                  type="password"
                  placeholder={settings.secret_key_set ? "••••••••" : ""}
                  value={form.secret_key}
                  onChange={(e) => set("secret_key", e.target.value)}
                  aria-invalid={Boolean(errors.secret_key)}
                  autoComplete="new-password"
                />
              </FormField>
            </div>
            <div className="flex items-center justify-between gap-4 rounded-lg border px-3 py-2.5">
              <label htmlFor="s3-path-style" className="min-w-0 cursor-pointer">
                <span className="block text-sm font-medium">Path-style addresses</span>
                <span className="text-muted-foreground block text-xs">
                  Turn on for MinIO and most self-hosted storage.
                </span>
              </label>
              <Switch
                id="s3-path-style"
                checked={form.path_style}
                onCheckedChange={(on) => set("path_style", on)}
              />
            </div>
          </Section>

          <Section title="Automatic backups">
            <FormField id="backup-schedule" label="Schedule" error={errors.schedule}>
              <div id="backup-schedule" role="radiogroup" className="flex flex-wrap gap-1.5">
                {schedules.map((s) => (
                  <Button
                    key={s.value}
                    type="button"
                    size="sm"
                    role="radio"
                    aria-checked={form.schedule === s.value}
                    variant={form.schedule === s.value ? "default" : "outline"}
                    onClick={() => set("schedule", s.value)}
                  >
                    {s.label}
                  </Button>
                ))}
              </div>
            </FormField>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField
                id="backup-hour"
                label="Time"
                error={errors.hour}
                hint={`Panel time (${settings.timezone}).`}
              >
                <select
                  id="backup-hour"
                  value={form.hour}
                  disabled={form.schedule === "off"}
                  onChange={(e) => set("hour", Number(e.target.value))}
                  className="border-input focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 h-8 w-full rounded-lg border bg-transparent px-2 text-base outline-none focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-50 md:text-sm"
                >
                  {hours.map((h) => (
                    <option key={h} value={h}>
                      {pad(h)}:00
                    </option>
                  ))}
                </select>
              </FormField>
              <FormField
                id="backup-keep"
                label="Keep"
                error={errors.keep}
                hint="The newest backups to keep; older ones are deleted. 0 keeps all."
              >
                <Input
                  id="backup-keep"
                  inputMode="numeric"
                  value={form.keep}
                  onChange={(e) => set("keep", e.target.value.replace(/\D/g, ""))}
                  aria-invalid={Boolean(errors.keep)}
                />
              </FormField>
            </div>
          </Section>
        </CardContent>
        <CardFooter className="flex-wrap justify-end gap-2">
          {settings.connected && (
            <Button
              type="button"
              variant="ghost"
              className="text-destructive mr-auto"
              disabled={busy !== undefined}
              onClick={() => setConfirmDisconnect(true)}
            >
              Disconnect
            </Button>
          )}
          <Button type="button" variant="outline" disabled={busy !== undefined} onClick={() => void run("test")}>
            {busy === "test" ? <Loader2 className="animate-spin" /> : <PlugZap />}
            Test connection
          </Button>
          <Button type="submit" disabled={busy !== undefined}>
            {busy === "save" && <Loader2 className="animate-spin" />}
            {settings.connected ? "Save" : "Connect"}
          </Button>
        </CardFooter>
      </form>
      <ConfirmDialog
        open={confirmDisconnect}
        onOpenChange={setConfirmDisconnect}
        title="Disconnect the bucket?"
        description="Automatic backups stop and the keys are removed from the panel. Backups already in the bucket stay there."
        confirmLabel="Disconnect"
        onConfirm={disconnect}
      />
    </Card>
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
