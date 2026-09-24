import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { Download, Loader2 } from "lucide-react"
import { QRCodeSVG } from "qrcode.react"
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
import { Skeleton } from "@/components/ui/skeleton"
import { CopyButton } from "@/components/copy-button"
import { FormField } from "@/components/form-field"
import { ApiError, api, peerConfigUrl } from "@/lib/api"
import type { Peer } from "@/lib/api"
import { errorMessage } from "@/lib/format"

interface NameProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  peer?: Peer
  onSaved: (result: SaveResult) => void
}

type SaveResult = { peer?: Peer; warning?: string }

export function PeerNameDialog({ open, onOpenChange, instanceId, peer, onSaved }: NameProps) {
  const [busy, setBusy] = useState(false)

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{peer ? "Rename peer" : "Add peer"}</DialogTitle>
          <DialogDescription>
            {peer
              ? "The name only shows up here and in the config file name."
              : "A peer is one device. It gets its own address and keys."}
          </DialogDescription>
        </DialogHeader>
        <PeerNameForm
          instanceId={instanceId}
          peer={peer}
          busy={busy}
          setBusy={setBusy}
          onDone={(result) => {
            onSaved(result)
            onOpenChange(false)
          }}
          onCancel={() => onOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  )
}

function PeerNameForm({ instanceId, peer, busy, setBusy, onDone, onCancel }: {
  instanceId: number
  peer?: Peer
  busy: boolean
  setBusy: (busy: boolean) => void
  onDone: (result: SaveResult) => void
  onCancel: () => void
}) {
  const [name, setName] = useState(peer?.name ?? "")
  const [error, setError] = useState("")

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")
    setBusy(true)
    try {
      const saved = peer
        ? await api.updatePeer(instanceId, peer.id, { name })
        : await api.createPeer(instanceId, { name })
      onDone({ peer: saved })
    } catch (err) {
      if (err instanceof ApiError && err.code === "apply_failed") {
        onDone({ warning: err.message })
        return
      }
      setError(err instanceof ApiError ? (err.fields.name ?? err.message) : errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <FormField id="peer-name" label="Name" error={error}>
        <Input
          id="peer-name"
          placeholder="Alice's phone"
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-invalid={Boolean(error)}
        />
      </FormField>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          {peer ? "Save" : "Add peer"}
        </Button>
      </DialogFooter>
    </form>
  )
}

interface ConfigProps {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function PeerConfigDialog({ peer, open, onOpenChange }: ConfigProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{peer?.name}</DialogTitle>
          <DialogDescription>
            Scan with the WireGuard app, or download the file for a desktop client. Anyone with
            this config can join the VPN as this peer.
          </DialogDescription>
        </DialogHeader>
        {peer && <PeerConfig key={peer.id} peer={peer} />}
      </DialogContent>
    </Dialog>
  )
}

function PeerConfig({ peer }: { peer: Peer }) {
  const [config, setConfig] = useState<string>()
  const [error, setError] = useState("")

  useEffect(() => {
    api
      .peerConfig(peer.instance_id, peer.id)
      .then(setConfig)
      .catch((err) => setError(errorMessage(err)))
  }, [peer.instance_id, peer.id])

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="space-y-4">
      {!peer.enabled && (
        <Alert>
          <AlertDescription>
            This peer is disabled. The config is valid but the server will refuse it until you
            enable the peer again.
          </AlertDescription>
        </Alert>
      )}
      <div className="flex justify-center">
        {config ? (
          // A white quiet zone keeps the code scannable in dark mode too.
          <div className="rounded-lg bg-white p-3">
            <QRCodeSVG value={config} size={224} marginSize={0} />
          </div>
        ) : (
          <Skeleton className="size-[248px] rounded-lg" />
        )}
      </div>
      <DialogFooter>
        {config && <CopyButton value={config} label="Copy config" showLabel />}
        <Button
          variant="outline"
          nativeButton={false}
          render={<a href={peerConfigUrl(peer.instance_id, peer.id)} download />}
        >
          <Download />
          Download .conf
        </Button>
      </DialogFooter>
    </div>
  )
}
