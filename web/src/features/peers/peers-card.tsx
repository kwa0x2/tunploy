import { useState } from "react"
import {
  ArrowRightLeft,
  ChartColumn,
  Download,
  Gauge,
  Link2,
  MoreHorizontal,
  Pencil,
  Plus,
  QrCode,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { DeviceIcon, Location, MonthUsage, PeerStatus } from "@/components/status"
import { PeerConfigDialog } from "@/features/peers/peer-config-dialog"
import { PeerLimitsDialog } from "@/features/peers/peer-limits-dialog"
import { PeerMoveDialog } from "@/features/peers/peer-move-dialog"
import { PeerNameDialog } from "@/features/peers/peer-name-dialog"
import { PeerShareDialog } from "@/features/peers/peer-share-dialog"
import { PeerUsageDialog } from "@/features/peers/peer-usage-dialog"
import { notApplied } from "@/features/peers/save-result"
import { useNow } from "@/hooks/use-now"
import { useTarget } from "@/hooks/use-target"
import { api, peerConfigUrl } from "@/api"
import type { Instance, Peer } from "@/api"
import {
  endpointHost,
  errorMessage,
  formatRelative,
  isOnline,
  lastSeen,
} from "@/lib/format"

export function PeersCard({ instance, peers, error, reload }: {
  instance: Instance
  peers?: Peer[]
  error: unknown
  reload: () => Promise<void>
}) {
  const [adding, setAdding] = useState(false)
  const renaming = useTarget<Peer>()
  const deleting = useTarget<Peer>()
  const showing = useTarget<Peer>()
  const usage = useTarget<Peer>()
  const limiting = useTarget<Peer>()
  const moving = useTarget<Peer>()
  const sharing = useTarget<Peer>()
  const [toggling, setToggling] = useState<number>()

  async function toggle(peer: Peer, enabled: boolean) {
    setToggling(peer.id)
    try {
      await api.updatePeer(instance.id, peer.id, { enabled })
    } catch (err) {
      const warning = notApplied(err)
      if (warning) toast.warning(warning)
      else toast.error(errorMessage(err))
    } finally {
      setToggling(undefined)
      void reload()
    }
  }

  const now = useNow()

  return (
    <Card className="self-start">
      <CardHeader>
        <CardTitle>Peers</CardTitle>
        <CardAction>
          <Button size="sm" onClick={() => setAdding(true)}>
            <Plus />
            Add peer
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {!peers && error !== undefined && (
          <Alert variant="destructive">
            <AlertDescription>{errorMessage(error)}</AlertDescription>
          </Alert>
        )}
        {!peers && error === undefined && <Skeleton className="h-24" />}
        {peers?.length === 0 && (
          <div className="text-muted-foreground py-8 text-center text-sm">
            No peers yet. Add one for each device that should join this VPN.
          </div>
        )}
        {peers && peers.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Location</TableHead>
                <TableHead title="This month, or toward the data limit when a device has one">Usage</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {peers.map((peer) => {
                const handshake = lastSeen(peer)
                return (
                  <TableRow key={peer.id}>
                    <TableCell className="max-w-48 font-medium">
                      <button
                        type="button"
                        onClick={() => usage.show(peer)}
                        className="flex max-w-full items-center gap-2.5 text-left underline-offset-4 hover:underline"
                      >
                        <DeviceIcon />
                        <span className="truncate">{peer.name}</span>
                      </button>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{peer.address}</TableCell>
                    <TableCell>
                      <PeerStatus
                        enabled={peer.enabled}
                        online={isOnline(peer)}
                        lastSeen={handshake && formatRelative(handshake, now)}
                        blocked={peer.blocked}
                      />
                    </TableCell>
                    <TableCell>
                      <Location
                        ip={peer.stats?.endpoint && endpointHost(peer.stats.endpoint)}
                        country={peer.country}
                      />
                    </TableCell>
                    <TableCell>
                      <MonthUsage peer={peer} />
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={peer.enabled}
                        disabled={toggling === peer.id}
                        onCheckedChange={(enabled) => void toggle(peer, enabled)}
                        aria-label={`${peer.enabled ? "Disable" : "Enable"} ${peer.name}`}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Show QR code for ${peer.name}`}
                          onClick={() => showing.show(peer)}
                        >
                          <QrCode />
                        </Button>
                        <DropdownMenu>
                          <DropdownMenuTrigger
                            render={
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label={`More actions for ${peer.name}`}
                              />
                            }
                          >
                            <MoreHorizontal />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end" className="w-44">
                            <DropdownMenuItem
                              render={<a href={peerConfigUrl(instance.id, peer.id)} download />}
                            >
                              <Download />
                              Download .conf
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => sharing.show(peer)}>
                              <Link2 />
                              Share link
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => usage.show(peer)}>
                              <ChartColumn />
                              Usage
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => limiting.show(peer)}>
                              <Gauge />
                              Limits
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => renaming.show(peer)}>
                              <Pencil />
                              Rename
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => moving.show(peer)}>
                              <ArrowRightLeft />
                              Move to server
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              variant="destructive"
                              onClick={() => deleting.show(peer)}
                            >
                              <Trash2 />
                              Delete
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <PeerNameDialog
        open={adding}
        onOpenChange={setAdding}
        instanceId={instance.id}
        onSaved={({ peer, warning }) => {
          if (warning) toast.warning(warning)
          if (peer) showing.show(peer)
          void reload()
        }}
      />
      <PeerNameDialog
        key={renaming.target?.id}
        open={renaming.open}
        onOpenChange={renaming.onOpenChange}
        instanceId={instance.id}
        peer={renaming.target}
        onSaved={({ warning }) => {
          if (warning) toast.warning(warning)
          void reload()
        }}
      />
      <PeerConfigDialog
        peer={showing.target}
        open={showing.open}
        onOpenChange={showing.onOpenChange}
        fullTunnel={instance.client_allowed_ips.includes("0.0.0.0/0")}
      />
      <PeerUsageDialog
        peer={peers?.find((p) => p.id === usage.target?.id) ?? usage.target}
        open={usage.open}
        onOpenChange={usage.onOpenChange}
        onEditLimits={(peer) => {
          usage.onOpenChange(false)
          limiting.show(peer)
        }}
      />
      <PeerLimitsDialog
        peer={limiting.target}
        open={limiting.open}
        onOpenChange={limiting.onOpenChange}
        onSaved={({ warning }) => {
          if (warning) toast.warning(warning)
          else toast.success("Limits saved.")
          void reload()
        }}
      />
      <PeerShareDialog peer={sharing.target} open={sharing.open} onOpenChange={sharing.onOpenChange} />
      <PeerMoveDialog
        peer={moving.target}
        open={moving.open}
        onOpenChange={moving.onOpenChange}
        onSaved={({ peer, warning }) => {
          if (warning) toast.warning(warning)
          if (peer) {
            toast.success(`${peer.name} moved. Give the device its new config.`)
            showing.show(peer)
          }
          void reload()
        }}
      />
      <ConfirmDialog
        open={deleting.open}
        onOpenChange={deleting.onOpenChange}
        title={`Delete ${deleting.target?.name}?`}
        description="The device is disconnected right away and its config stops working."
        confirmLabel="Delete peer"
        onConfirm={async () => {
          if (!deleting.target) return
          try {
            await api.deletePeer(instance.id, deleting.target.id)
          } catch (err) {
            const warning = notApplied(err)
            if (!warning) throw err
            toast.warning(warning)
          }
          void reload()
        }}
      />
    </Card>
  )
}
