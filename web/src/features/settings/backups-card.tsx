import { useCallback, useRef, useState } from "react"
import {
  ArchiveRestore,
  CloudUpload,
  DatabaseBackup,
  Download,
  Loader2,
  LockKeyhole,
  LockKeyholeOpen,
  RefreshCw,
  Trash2,
  Upload,
} from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { PassphraseDialog, RestoreDialog } from "@/features/settings/backup-dialogs"
import type { RestoreTarget } from "@/features/settings/backup-dialogs"
import { useAuth } from "@/features/auth/auth-context"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { api, backupExportUrl, backupFileUrl } from "@/api"
import type { BackupObject, BackupSettings, RestoreResult } from "@/api"
import { errorMessage, formatBytes, formatDateTime, formatRelative } from "@/lib/format"

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
