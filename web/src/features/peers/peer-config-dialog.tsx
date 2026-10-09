import { useEffect, useState } from "react"
import { ChevronDown, Download, ShieldCheck } from "lucide-react"
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
import { Skeleton } from "@/components/ui/skeleton"
import { CopyButton } from "@/components/copy-button"
import { api, peerConfigUrl } from "@/api"
import type { Peer } from "@/api"
import { errorMessage } from "@/lib/format"

interface ConfigProps {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
  // Whether the server's clients send everything through it; a kill switch needs that.
  fullTunnel: boolean
}

export function PeerConfigDialog({ peer, open, onOpenChange, fullTunnel }: ConfigProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{peer?.name}</DialogTitle>
          <DialogDescription>
            {peer?.key_on_client
              ? "The server side of this device's config."
              : "Scan with the WireGuard app, or download the file for a desktop client. Anyone with this config can join the VPN as this peer."}
          </DialogDescription>
        </DialogHeader>
        {peer && <PeerConfig key={peer.id} peer={peer} fullTunnel={fullTunnel} />}
      </DialogContent>
    </Dialog>
  )
}

function PeerConfig({ peer, fullTunnel }: { peer: Peer; fullTunnel: boolean }) {
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
      {peer.key_on_client && (
        <Alert>
          <AlertDescription>
            This device made its own key pair, so the panel never saw its private key. The config
            below has no PrivateKey line; the app that owns the key fills it in.
          </AlertDescription>
        </Alert>
      )}
      <div className="flex justify-center">
        {config && peer.key_on_client ? (
          <pre className="bg-muted max-h-64 w-full overflow-auto rounded-lg p-3 font-mono text-xs">
            {config}
          </pre>
        ) : config ? (
          // A white quiet zone keeps the code scannable in dark mode too.
          <div className="rounded-lg bg-white p-3">
            <QRCodeSVG value={config} size={224} marginSize={0} />
          </div>
        ) : (
          <Skeleton className="size-[248px] rounded-lg" />
        )}
      </div>
      <KillSwitchHelp peer={peer} fullTunnel={fullTunnel} />
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

// WireGuard's apps each have their own switch; only wg-quick on Linux needs
// it written into the config.
function KillSwitchHelp({ peer, fullTunnel }: { peer: Peer; fullTunnel: boolean }) {
  return (
    <details className="group rounded-lg border p-3 text-sm">
      <summary className="flex cursor-pointer list-none items-center justify-between font-medium">
        <span className="flex items-center gap-2">
          <ShieldCheck className="size-4" />
          Kill switch
        </span>
        <ChevronDown className="text-muted-foreground size-4 transition-transform group-open:rotate-180" />
      </summary>
      {fullTunnel ? (
        <div className="mt-3 space-y-2">
          <p className="text-muted-foreground">
            Blocks the device&apos;s internet while the VPN is down, so nothing leaks past it.
          </p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
            <dt className="font-medium">Windows</dt>
            <dd className="text-muted-foreground">
              On by default: &quot;Block untunneled traffic&quot; in the tunnel&apos;s settings.
            </dd>
            <dt className="font-medium">Android</dt>
            <dd className="text-muted-foreground">
              Settings → Network → VPN → WireGuard ⚙ → Always-on VPN and Block connections without
              VPN.
            </dd>
            <dt className="font-medium">iPhone, Mac</dt>
            <dd className="text-muted-foreground">
              Turn on On-Demand for the tunnel in the WireGuard app, so it reconnects on every
              network. Apple has no full block for WireGuard.
            </dd>
            <dt className="font-medium">Linux</dt>
            <dd className="text-muted-foreground">
              Use the download below with <code className="font-mono text-xs">wg-quick</code>; it adds
              firewall rules while the tunnel is up.
            </dd>
          </dl>
          <Button
            variant="outline"
            size="sm"
            nativeButton={false}
            render={<a href={peerConfigUrl(peer.instance_id, peer.id, true)} download />}
          >
            <Download />
            Download for Linux with kill switch
          </Button>
        </div>
      ) : (
        <p className="text-muted-foreground mt-3">
          A kill switch blocks everything outside the VPN, but this server only carries some
          traffic. Set its client allowed IPs to 0.0.0.0/0 to use one.
        </p>
      )}
    </details>
  )
}
