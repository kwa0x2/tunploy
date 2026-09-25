import { useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
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
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/lib/api"
import type { BackupSettings, RestoreResult } from "@/lib/api"
import { errorMessage } from "@/lib/format"

const minPassphrase = 12

export function PassphraseDialog({ open, onOpenChange, encrypted, onSaved }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  encrypted: boolean
  onSaved: (settings: BackupSettings) => void
}) {
  const [busy, setBusy] = useState(false)
  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{encrypted ? "Change the backup passphrase" : "Encrypt backups"}</DialogTitle>
          <DialogDescription>
            New backups, downloaded or in the bucket, are encrypted with this passphrase. Backups made
            before keep the passphrase they were made with.
          </DialogDescription>
        </DialogHeader>
        <PassphraseForm busy={busy} setBusy={setBusy} onSaved={onSaved} onCancel={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function PassphraseForm({ busy, setBusy, onSaved, onCancel }: {
  busy: boolean
  setBusy: (busy: boolean) => void
  onSaved: (settings: BackupSettings) => void
  onCancel: () => void
}) {
  const [passphrase, setPassphrase] = useState("")
  const [repeat, setRepeat] = useState("")
  const [errors, setErrors] = useState<Record<string, string>>({})

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    if ([...passphrase].length < minPassphrase) {
      setErrors({ passphrase: `Use at least ${minPassphrase} characters.` })
      return
    }
    if (passphrase !== repeat) {
      setErrors({ repeat: "The passphrases do not match." })
      return
    }
    setErrors({})
    setBusy(true)
    try {
      onSaved(await api.setBackupEncryption(passphrase))
    } catch (err) {
      setErrors({ passphrase: err instanceof ApiError ? (err.fields.passphrase ?? err.message) : errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <Alert>
        <AlertDescription>
          Keep it in a password manager. Without it an encrypted backup cannot be restored: not by
          you, not by a reinstalled panel, not by anyone.
        </AlertDescription>
      </Alert>
      {/* Lets password managers file it under something recognisable. */}
      <input type="text" autoComplete="username" value="Tunploy backups" readOnly hidden />
      <FormField id="backup-passphrase" label="Passphrase" error={errors.passphrase} hint={`At least ${minPassphrase} characters.`}>
        <Input
          id="backup-passphrase"
          type="password"
          autoComplete="new-password"
          autoFocus
          value={passphrase}
          onChange={(e) => setPassphrase(e.target.value)}
          aria-invalid={Boolean(errors.passphrase)}
        />
      </FormField>
      <FormField id="backup-passphrase-repeat" label="Repeat passphrase" error={errors.repeat}>
        <Input
          id="backup-passphrase-repeat"
          type="password"
          autoComplete="new-password"
          value={repeat}
          onChange={(e) => setRepeat(e.target.value)}
          aria-invalid={Boolean(errors.repeat)}
        />
      </FormField>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          Save passphrase
        </Button>
      </DialogFooter>
    </form>
  )
}

export interface RestoreTarget {
  name: string
  // Known for bucket backups; a file is only guessed from its name until the panel reads it.
  encrypted: boolean
  run: (passphrase?: string) => Promise<RestoreResult>
}

export function RestoreDialog({ target, onOpenChange, panelEncrypted, onRestored }: {
  target?: RestoreTarget
  onOpenChange: (open: boolean) => void
  panelEncrypted: boolean
  onRestored: (result: RestoreResult) => void
}) {
  const [busy, setBusy] = useState(false)
  return (
    <Dialog open={target !== undefined} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="pr-6 break-all">Restore {target?.name}?</DialogTitle>
          <DialogDescription>
            Everything on this panel is replaced with the backup: servers, devices, settings, users and
            history. VPN servers restart, so devices drop for a moment. You will be signed out. Backup
            storage settings and the backup passphrase stay as they are.
          </DialogDescription>
        </DialogHeader>
        {target && (
          <RestoreForm
            key={target.name}
            target={target}
            panelEncrypted={panelEncrypted}
            busy={busy}
            setBusy={setBusy}
            onRestored={onRestored}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

const askAgainCodes = new Set(["passphrase_required", "wrong_passphrase"])

function RestoreForm({ target, panelEncrypted, busy, setBusy, onRestored, onCancel }: {
  target: RestoreTarget
  panelEncrypted: boolean
  busy: boolean
  setBusy: (busy: boolean) => void
  onRestored: (result: RestoreResult) => void
  onCancel: () => void
}) {
  const [askPassphrase, setAskPassphrase] = useState(target.encrypted)
  const [passphrase, setPassphrase] = useState("")
  const [passphraseError, setPassphraseError] = useState("")
  const [error, setError] = useState("")

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")
    setPassphraseError("")
    setBusy(true)
    try {
      onRestored(await target.run(passphrase || undefined))
    } catch (err) {
      if (err instanceof ApiError && askAgainCodes.has(err.code)) {
        setAskPassphrase(true)
        setPassphraseError(
          err.code === "wrong_passphrase" && !passphrase
            ? "This panel's passphrase does not open this backup. Enter the one it was made with."
            : err.message,
        )
      } else {
        setError(errorMessage(err))
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {askPassphrase && (
        <FormField
          id="restore-passphrase"
          label="Backup passphrase"
          error={passphraseError}
          hint={panelEncrypted ? "Leave empty to use this panel's passphrase." : "The passphrase the backup was made with."}
        >
          <Input
            id="restore-passphrase"
            type="password"
            autoComplete="current-password"
            autoFocus
            value={passphrase}
            onChange={(e) => setPassphrase(e.target.value)}
            aria-invalid={Boolean(passphraseError)}
          />
        </FormField>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" variant="destructive" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          Restore
        </Button>
      </DialogFooter>
    </form>
  )
}
