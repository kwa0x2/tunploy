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
import { Switch } from "@/components/ui/switch"
import { CopyButton } from "@/components/copy-button"
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/api"
import type { ApiScope, CreatedApiKey } from "@/api"
import { errorMessage } from "@/lib/format"
import { cn } from "@/lib/utils"

const scopes: { id: ApiScope; label: string; hint: string }[] = [
  { id: "devices:read", label: "Read devices", hint: "List devices, their configs and usage" },
  { id: "devices:write", label: "Manage devices", hint: "Create, change, move, disable and delete devices" },
  { id: "servers:read", label: "Read servers", hint: "List servers and nodes, their status and free space" },
  { id: "servers:write", label: "Manage servers", hint: "Create, change and delete servers, and the devices on them" },
  { id: "events:read", label: "Read events", hint: "Connections and changes to devices, servers and nodes" },
  { id: "webhooks:write", label: "Manage webhooks", hint: "Add, change and remove webhooks, and see their deliveries" },
]

const expiries = [
  { label: "Never", days: 0 },
  { label: "30 days", days: 30 },
  { label: "90 days", days: 90 },
  { label: "1 year", days: 365 },
]

interface CreateProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}

export function CreateKeyDialog({ open, onOpenChange, onCreated }: CreateProps) {
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<CreatedApiKey>()

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (busy) return
        onOpenChange(next)
        if (!next) setCreated(undefined)
      }}
    >
      <DialogContent>
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Copy your key</DialogTitle>
              <DialogDescription>
                This is the only time the panel shows it. Store it somewhere safe; if you lose it,
                revoke it and create a new one.
              </DialogDescription>
            </DialogHeader>
            <div className="flex items-center gap-2">
              <code className="bg-muted min-w-0 flex-1 rounded-md px-3 py-2 font-mono text-xs break-all">
                {created.token}
              </code>
              <CopyButton value={created.token} label="Copy key" />
            </div>
            <DialogFooter>
              <Button
                onClick={() => {
                  onOpenChange(false)
                  setCreated(undefined)
                }}
              >
                Done
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Create API key</DialogTitle>
              <DialogDescription>Name it after the program that will use it.</DialogDescription>
            </DialogHeader>
            <CreateKeyForm
              key={String(open)}
              busy={busy}
              setBusy={setBusy}
              onCancel={() => onOpenChange(false)}
              onCreated={(k) => {
                setCreated(k)
                onCreated()
              }}
            />
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

function CreateKeyForm({ busy, setBusy, onCancel, onCreated }: {
  busy: boolean
  setBusy: (busy: boolean) => void
  onCancel: () => void
  onCreated: (key: CreatedApiKey) => void
}) {
  const [name, setName] = useState("")
  const [chosen, setChosen] = useState<ApiScope[]>(["devices:read", "devices:write", "servers:read"])
  const [days, setDays] = useState(0)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setErrors({})
    setFormError("")
    setBusy(true)
    try {
      const expires_at = days ? new Date(Date.now() + days * 86_400_000).toISOString() : undefined
      onCreated(await api.createApiKey({ name, scopes: chosen, expires_at }))
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
      <FormField id="key-name" label="Name" error={errors.name}>
        <Input
          id="key-name"
          placeholder="billing-backend"
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-invalid={Boolean(errors.name)}
        />
      </FormField>
      <div className="space-y-2">
        <p className="text-sm font-medium">Scopes</p>
        {errors.scopes && <p className="text-destructive text-sm">{errors.scopes}</p>}
        <ul className="divide-y rounded-lg border">
          {scopes.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-4 px-3 py-2.5">
              <label htmlFor={`scope-${s.id}`} className="min-w-0 cursor-pointer">
                <span className="block text-sm font-medium">{s.label}</span>
                <span className="text-muted-foreground block text-xs">{s.hint}</span>
              </label>
              <Switch
                id={`scope-${s.id}`}
                checked={chosen.includes(s.id)}
                onCheckedChange={(on) =>
                  setChosen((c) => (on ? [...c, s.id] : c.filter((x) => x !== s.id)))
                }
              />
            </li>
          ))}
        </ul>
      </div>
      <div className="space-y-2">
        <p className="text-sm font-medium">Expires</p>
        <div className="flex flex-wrap gap-1.5">
          {expiries.map((e) => (
            <Button
              key={e.days}
              type="button"
              size="sm"
              variant={days === e.days ? "default" : "outline"}
              className={cn(days === e.days && "pointer-events-none")}
              onClick={() => setDays(e.days)}
            >
              {e.label}
            </Button>
          ))}
        </div>
        {errors.expires_at && <p className="text-destructive text-sm">{errors.expires_at}</p>}
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy || !name.trim() || chosen.length === 0}>
          {busy && <Loader2 className="animate-spin" />}
          Create key
        </Button>
      </DialogFooter>
    </form>
  )
}
