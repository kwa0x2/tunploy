import { useEffect, useState } from "react"
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
import { Skeleton } from "@/components/ui/skeleton"
import { CopyButton } from "@/components/copy-button"
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/api"
import type { Peer, ShareLink } from "@/api"
import { errorMessage, formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"

interface ShareProps {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function PeerShareDialog({ peer, open, onOpenChange }: ShareProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Share {peer?.name}</DialogTitle>
          <DialogDescription>
            A page where the device&apos;s owner scans its QR code and sees how much data is left,
            without signing in. It always shows the current config, even after a move. Anyone with
            the link can use the config, so send it only to them.
          </DialogDescription>
        </DialogHeader>
        {peer && <PeerShare key={peer.id} peer={peer} />}
      </DialogContent>
    </Dialog>
  )
}

const linkDurations = [
  { label: "1 day", days: 1 },
  { label: "1 week", days: 7 },
  { label: "1 month", days: 30 },
  { label: "Until removed", days: 0 },
]

function PeerShare({ peer }: { peer: Peer }) {
  // undefined while loading, null when there is none.
  const [link, setLink] = useState<ShareLink | null>()
  const [days, setDays] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    api
      .peerShare(peer.instance_id, peer.id)
      .then(setLink)
      .catch((err) => {
        if (err instanceof ApiError && err.status === 404) setLink(null)
        else setError(errorMessage(err))
      })
  }, [peer.instance_id, peer.id])

  async function run(action: () => Promise<ShareLink | null>) {
    setError("")
    setBusy(true)
    try {
      setLink(await action())
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const create = () =>
    run(() =>
      api.createPeerShare(
        peer.instance_id,
        peer.id,
        days ? new Date(Date.now() + days * 86_400_000).toISOString() : null,
      ),
    )
  const remove = () => run(() => api.deletePeerShare(peer.instance_id, peer.id).then(() => null))
  const expired = link?.expires_at && new Date(link.expires_at) <= new Date()

  return (
    <div className="space-y-4">
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {link === undefined && !error && <Skeleton className="h-8" />}
      {link && (
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5">
            <Input
              readOnly
              value={link.url}
              aria-label="Share link"
              onFocus={(e) => e.target.select()}
              className="font-mono text-xs"
            />
            <CopyButton value={link.url} label="Copy link" />
          </div>
          <p className={cn("text-xs", expired ? "text-red-700 dark:text-red-400" : "text-muted-foreground")}>
            {link.expires_at
              ? `${expired ? "Stopped working" : "Works until"} ${formatDateTime(link.expires_at)}.`
              : "Works until you remove it."}
          </p>
        </div>
      )}
      {link === null && <p className="text-muted-foreground text-sm">This device has no share link yet.</p>}
      {link?.url.startsWith("http://") && (
        <Alert>
          <AlertDescription>
            The link is plain HTTP, so the config travels unencrypted. Give the panel a domain under
            Settings → Domain and make a new link.
          </AlertDescription>
        </Alert>
      )}
      {peer.key_on_client && (
        <Alert>
          <AlertDescription>
            This device made its own key pair, so the page shows its usage but no QR code or
            config.
          </AlertDescription>
        </Alert>
      )}
      {link !== undefined && (
        <FormField
          id="share-duration"
          label={link ? "A new link works for" : "The link works for"}
          hint={link ? "A new link stops the one above from working." : undefined}
        >
          <div role="radiogroup" aria-label="Link works for" className="flex flex-wrap gap-1.5">
            {linkDurations.map((d) => (
              <Button
                key={d.days}
                type="button"
                size="xs"
                role="radio"
                aria-checked={days === d.days}
                variant={days === d.days ? "default" : "outline"}
                onClick={() => setDays(d.days)}
              >
                {d.label}
              </Button>
            ))}
          </div>
        </FormField>
      )}
      <DialogFooter>
        {link && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void remove()}>
            Remove link
          </Button>
        )}
        <Button type="button" disabled={busy || link === undefined} onClick={() => void create()}>
          {busy && <Loader2 className="animate-spin" />}
          {link ? "Make a new link" : "Create link"}
        </Button>
      </DialogFooter>
    </div>
  )
}
