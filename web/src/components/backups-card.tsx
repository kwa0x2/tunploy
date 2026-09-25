import { useCallback, useRef, useState } from "react"
import type { FormEvent, ReactNode } from "react"
import {
  ArchiveRestore,
  CloudUpload,
  DatabaseBackup,
  Download,
  Loader2,
  LockKeyhole,
  LockKeyholeOpen,
  PlugZap,
  RefreshCw,
  Trash2,
  Upload,
} from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
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
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { PassphraseDialog, RestoreDialog } from "@/components/backup-dialogs"
import type { RestoreTarget } from "@/components/backup-dialogs"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { FormField } from "@/components/form-field"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { ApiError, api, backupExportUrl, backupFileUrl } from "@/lib/api"
import type { BackupInput, BackupObject, BackupSchedule, BackupSettings, RestoreResult } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { errorMessage, formatBytes, formatDateTime, formatRelative } from "@/lib/format"

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

export function BackupsCard({ settings, onChange }: { settings: BackupSettings; onChange: () => void }) {
  const { recheck } = useAuth()
  const now = useNow()
  const fileInput = useRef<HTMLInputElement>(null)
  const [restoring, setRestoring] = useState<RestoreTarget>()
  const [deleting, setDeleting] = useState<BackupObject>()
  const [passphraseOpen, setPassphraseOpen] = useState(false)
  const [confirmPlain, setConfirmPlain] = useState(false)
  const [creating, setCreating] = useState(false)

  const connected = settings.connected
  const list = useResource(useCallback(() => (connected ? api.backups() : Promise.resolve([])), [connected]))

  async function createNow() {
    setCreating(true)
    try {
      const made = await api.createBackup()
      toast.success(`Backup ${made.name} (${formatBytes(made.size)}) stored in ${settings.bucket}.`)
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setCreating(false)
      onChange()
      void list.reload()
    }
  }

  function chooseFile(files: FileList | null) {
    const file = files?.[0]
    if (fileInput.current) fileInput.current.value = ""
    if (!file) return
    setRestoring({
      name: file.name,
      encrypted: file.name.endsWith(".enc"),
      run: (passphrase) => api.importBackup(file, passphrase),
    })
  }

  function restoreRemote(backup: BackupObject) {
    setRestoring({
      name: backup.name,
      encrypted: backup.encrypted,
      run: (passphrase) => api.restoreBackup(backup.name, passphrase),
    })
  }

  async function restored(res: RestoreResult) {
    setRestoring(undefined)
    toast.success(`Restored ${res.name}, made ${formatDateTime(res.created_at)}. Sign in again to continue.`, {
      duration: 10_000,
    })
    for (const w of res.warnings) toast.warning(w, { duration: 15_000 })
    await recheck()
  }

  async function deleteBackup() {
    if (!deleting) return
    await api.deleteBackup(deleting.name)
    toast.success(`Deleted ${deleting.name}.`)
    void list.reload()
  }

  async function turnOffEncryption() {
    await api.setBackupEncryption("")
    toast.success("New backups will not be encrypted.")
    onChange()
  }

  const status = settings.status

  return (
    <Card>
      <CardHeader>
        <CardTitle>Backups</CardTitle>
        <CardDescription>
          A backup holds every server and device with their keys, the panel settings, users and
          history. Anyone who has an unencrypted one can run your VPN, so keep it private.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap gap-2">
          {connected && (
            <Button disabled={creating} onClick={() => void createNow()}>
              {creating ? <Loader2 className="animate-spin" /> : <CloudUpload />}
              Back up to {settings.bucket}
            </Button>
          )}
          <Button variant="outline" nativeButton={false} render={<a href={backupExportUrl} download />}>
            <Download />
            Download backup
          </Button>
          <Button variant="outline" onClick={() => fileInput.current?.click()}>
            <Upload />
            Restore from file
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept=".gz,.tgz,.enc,application/gzip"
            className="hidden"
            onChange={(e) => chooseFile(e.target.files)}
          />
        </div>

        <div className="flex flex-wrap items-center gap-3 rounded-lg border px-3 py-2.5">
          <div className="flex min-w-60 flex-1 items-center gap-3">
            {settings.encrypted ? (
              <LockKeyhole className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
            ) : (
              <LockKeyholeOpen className="text-muted-foreground size-4 shrink-0" />
            )}
            <div className="min-w-0">
              <p className="text-sm font-medium">
                {settings.encrypted ? "Backups are encrypted" : "Backups are not encrypted"}
              </p>
              <p className="text-muted-foreground text-xs">
                {settings.encrypted
                  ? "With a passphrase saved on this panel. Restoring on a new server asks for it."
                  : "Set a passphrase so a leaked file or bucket does not give away your VPN."}
              </p>
            </div>
          </div>
          <div className="ml-auto flex shrink-0 gap-1.5">
            {settings.encrypted && (
              <Button variant="ghost" size="sm" className="text-destructive" onClick={() => setConfirmPlain(true)}>
                Turn off
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={() => setPassphraseOpen(true)}>
              {settings.encrypted ? "Change passphrase" : "Set passphrase"}
            </Button>
          </div>
        </div>

        {status.last_error && status.last_error_at && (
          <p className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-400">
            The last backup failed {formatRelative(status.last_error_at, now)}: {status.last_error}
          </p>
        )}
        {connected && (
          <p className="text-muted-foreground text-xs">
            {status.last_backup_at
              ? `Last backup ${formatRelative(status.last_backup_at, now)} (${formatBytes(status.last_backup_size ?? 0)}).`
              : "No backup in the bucket yet."}{" "}
            {status.next_run_at
              ? `Next automatic backup ${formatDateTime(status.next_run_at)}.`
              : "Automatic backups are off."}
          </p>
        )}

        {connected && (
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
                In {settings.bucket}
              </h3>
              <Button variant="ghost" size="icon-sm" aria-label="Refresh" onClick={() => void list.reload()}>
                <RefreshCw className={list.loading ? "animate-spin" : undefined} />
              </Button>
            </div>
            <RemoteList list={list.data} error={list.error} onRestore={restoreRemote} onDelete={setDeleting} />
          </div>
        )}
      </CardContent>

      <RestoreDialog
        target={restoring}
        onOpenChange={(open) => !open && setRestoring(undefined)}
        panelEncrypted={settings.encrypted}
        onRestored={(res) => void restored(res)}
      />
      <PassphraseDialog
        open={passphraseOpen}
        onOpenChange={setPassphraseOpen}
        encrypted={settings.encrypted}
        onSaved={() => {
          setPassphraseOpen(false)
          toast.success("Passphrase saved. New backups will be encrypted with it.")
          onChange()
        }}
      />
      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(open) => !open && setDeleting(undefined)}
        title={`Delete ${deleting?.name}?`}
        description="The file is removed from the bucket for good."
        confirmLabel="Delete"
        onConfirm={deleteBackup}
      />
      <ConfirmDialog
        open={confirmPlain}
        onOpenChange={setConfirmPlain}
        title="Stop encrypting backups?"
        description="New backups are stored without a passphrase and the saved one is removed from the panel. Backups made before stay encrypted and still need it."
        confirmLabel="Turn off"
        onConfirm={turnOffEncryption}
      />
    </Card>
  )
}

function RemoteList({ list, error, onRestore, onDelete }: {
  list?: BackupObject[]
  error: unknown
  onRestore: (b: BackupObject) => void
  onDelete: (b: BackupObject) => void
}) {
  if (error !== undefined) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{errorMessage(error)}</AlertDescription>
      </Alert>
    )
  }
  if (!list) return <Skeleton className="h-24 rounded-lg" />
  if (list.length === 0) {
    return (
      <div className="text-muted-foreground flex items-center gap-3 rounded-lg border border-dashed px-3 py-4 text-sm">
        <DatabaseBackup className="size-4 shrink-0" />
        No backups yet. Back up now, or turn on automatic backups above.
      </div>
    )
  }
  return (
    <ul className="divide-y rounded-lg border">
      {list.map((b) => (
        <li key={b.key} className="flex items-center gap-3 px-3 py-2">
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium" title={b.name}>
              {formatDateTime(b.modified)}
            </p>
            <p className="text-muted-foreground flex items-center gap-1 truncate text-xs" title={b.key}>
              {b.encrypted && <LockKeyhole className="size-3 shrink-0" aria-label="Encrypted" />}
              {formatBytes(b.size)} · {b.name}
            </p>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Download ${b.name}`}
            nativeButton={false}
            render={<a href={backupFileUrl(b.name)} download />}
          >
            <Download />
          </Button>
          <Button variant="ghost" size="icon-sm" aria-label={`Restore ${b.name}`} onClick={() => onRestore(b)}>
            <ArchiveRestore />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-destructive"
            aria-label={`Delete ${b.name}`}
            onClick={() => onDelete(b)}
          >
            <Trash2 />
          </Button>
        </li>
      ))}
    </ul>
  )
}
