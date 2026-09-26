import { useCallback, useState } from "react"
import type { ReactNode } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import {
  ArrowLeft,
  ArrowRightLeft,
  ChartColumn,
  Download,
  Gauge,
  HardDrive,
  Link2,
  Loader2,
  MoreHorizontal,
  Pencil,
  Play,
  Plus,
  QrCode,
  RotateCw,
  ScrollText,
  Settings2,
  Square,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import { CopyButton } from "@/components/copy-button"
import { InstanceFormDialog } from "@/components/instance-form-dialog"
import { LogsDialog } from "@/components/logs-dialog"
import { PageHeader } from "@/components/page-header"
import {
  PeerConfigDialog,
  PeerLimitsDialog,
  PeerMoveDialog,
  PeerNameDialog,
  PeerShareDialog,
} from "@/components/peer-dialogs"
import { PeerUsageDialog } from "@/components/peer-usage-dialog"
import { DeviceIcon, InstanceStatus, Location, MonthUsage, PeerStatus } from "@/components/status"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { useTarget } from "@/hooks/use-target"
import { ApiError, api, peerConfigUrl } from "@/lib/api"
import type { Instance, Peer } from "@/lib/api"
import {
  endpointHost,
  endpointOf,
  errorMessage,
  formatRelative,
  isOnline,
  lastSeen,
  locationText,
} from "@/lib/format"

const pollMs = 5_000

export function ServerDetailPage() {
  const id = Number(useParams().id)
  if (!Number.isInteger(id) || id <= 0) return <ServerMissing />
  // Keyed so switching servers doesn't flash the previous one's data.
  return <ServerDetail key={id} id={id} />
}

function ServerMissing() {
  return (
    <Card>
      <CardContent className="flex flex-col items-center gap-4 py-12 text-center">
        <p className="font-medium">This server does not exist</p>
        <Button variant="outline" nativeButton={false} render={<Link to="/servers" />}>
          Back to servers
        </Button>
      </CardContent>
    </Card>
  )
}

type Action = "start" | "stop" | "restart"

function ServerDetail({ id }: { id: number }) {
  const navigate = useNavigate()
  const instance = useResource(
    useCallback(() => api.instance(id), [id]),
    pollMs,
  )
  const peers = useResource(
    useCallback(() => api.peers(id), [id]),
    pollMs,
  )

  const nodes = useResource(api.nodes, 30_000)
  const [pending, setPending] = useState<Action | null>(null)
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [viewingLogs, setViewingLogs] = useState(false)

  if (instance.error instanceof ApiError && instance.error.status === 404) return <ServerMissing />

  const current = instance.data
  if (!current) {
    return instance.error ? (
      <Alert variant="destructive">
        <AlertDescription>{errorMessage(instance.error)}</AlertDescription>
      </Alert>
    ) : (
      <div className="space-y-6">
        <Skeleton className="h-12 w-64" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    )
  }

  async function run(action: Action) {
    setPending(action)
    try {
      await api.instanceAction(id, action)
      toast.success(
        { start: "Server started.", stop: "Server stopped.", restart: "Server restarted." }[action],
      )
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setPending(null)
      void instance.reload()
      void peers.reload()
    }
  }

  const { state } = current.status
  const up = state === "running" || state === "restarting"
  const node = current.node_id ? nodes.data?.find((n) => n.id === current.node_id) : undefined
  const nodeOffline = current.status.error === "node is offline"

  return (
    <>
      <Link
        to="/servers"
        className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm"
      >
        <ArrowLeft className="size-4" />
        Servers
      </Link>

      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            {current.name}
            <InstanceStatus state={state} />
          </span>
        }
        description={
          <span className="flex flex-wrap items-center gap-x-2">
            <span className="font-mono text-xs">{endpointOf(current)}</span>
            {node && (
              <Link to="/nodes" className="hover:text-foreground inline-flex items-center gap-1 text-xs">
                <HardDrive className="size-3.5" />
                {node.name}
              </Link>
            )}
            {locationText(current) && <span className="text-xs">{locationText(current)}</span>}
          </span>
        }
        actions={
          <div className="flex items-center gap-2">
            {up ? (
              <ActionButton action="stop" pending={pending} disabled={nodeOffline} onRun={run} icon={<Square />}>
                Stop
              </ActionButton>
            ) : (
              <ActionButton action="start" pending={pending} disabled={nodeOffline} onRun={run} icon={<Play />}>
                {state === "not_deployed" ? "Deploy" : "Start"}
              </ActionButton>
            )}
            <ActionButton action="restart" pending={pending} disabled={nodeOffline} onRun={run} icon={<RotateCw />}>
              Restart
            </ActionButton>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="outline" size="icon" aria-label="More actions" />}
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuItem onClick={() => setViewingLogs(true)}>
                  <ScrollText />
                  View logs
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setEditing(true)}>
                  <Settings2 />
                  Settings
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={() => setDeleting(true)}>
                  <Trash2 />
                  Delete server
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        }
      />

      {nodeOffline && (
        <Alert className="mb-6">
          <AlertTitle>{node ? `${node.name} is offline` : "The node is offline"}</AlertTitle>
          <AlertDescription>
            The panel can't reach the machine over SSH, so live stats are missing. The VPN keeps running there,
            and changes you make now are applied as soon as the panel reaches it again.
          </AlertDescription>
        </Alert>
      )}

      {current.status.error && !nodeOffline && (
        <Alert variant="destructive" className="mb-6">
          <AlertTitle>The tunnel is not healthy</AlertTitle>
          <AlertDescription>
            <p>{current.status.error}</p>
            {state !== "unknown" && (
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() => setViewingLogs(true)}
              >
                <ScrollText />
                View logs
              </Button>
            )}
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <PeersCard instance={current} peers={peers.data} error={peers.error} reload={peers.reload} />
        <ConnectionCard instance={current} />
      </div>

      <LogsDialog
        instanceId={id}
        name={current.name}
        open={viewingLogs}
        onOpenChange={setViewingLogs}
      />

      <InstanceFormDialog
        mode="edit"
        instance={current}
        open={editing}
        onOpenChange={setEditing}
        onSaved={() => {
          toast.success("Settings saved.")
          void instance.reload()
        }}
      />

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete ${current.name}?`}
        description={
          current.peer_count > 0
            ? `This removes the container and all ${current.peer_count} peers. Their configs stop working and cannot be recovered.`
            : "This removes the container and its configuration."
        }
        confirmLabel="Delete server"
        onConfirm={async () => {
          await api.deleteInstance(id)
          toast.success(`${current.name} deleted.`)
          navigate("/servers")
        }}
      />
    </>
  )
}

function ActionButton({ action, pending, onRun, icon, children, disabled }: {
  action: Action
  pending: Action | null
  onRun: (action: Action) => void
  icon: ReactNode
  children: ReactNode
  disabled?: boolean
}) {
  return (
    <Button variant="outline" disabled={disabled || pending !== null} onClick={() => onRun(action)}>
      {pending === action ? <Loader2 className="animate-spin" /> : icon}
      {children}
    </Button>
  )
}

function ConnectionCard({ instance }: { instance: Instance }) {
  const rows: [string, ReactNode, string?][] = [
    ["Endpoint", endpointOf(instance), endpointOf(instance)],
    ["Server address", instance.address],
    ["Public key", instance.public_key, instance.public_key],
    [
      "DNS",
      instance.dns_on_server
        ? `${instance.address.split("/")[0]} on this server, forwarding to ${instance.dns.join(", ")}`
        : instance.dns.length
          ? instance.dns.join(", ")
          : "None",
    ],
    ["MTU", instance.mtu || "1420"],
    ["Keepalive", instance.persistent_keepalive ? `${instance.persistent_keepalive}s` : "Off"],
    ["Routed", instance.client_allowed_ips.join(", ")],
  ]

  return (
    <Card className="self-start">
      <CardHeader>
        <CardTitle>Connection</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="space-y-3 text-sm">
          {rows.map(([label, value, copy]) => (
            <div key={label} className="space-y-0.5">
              <dt className="text-muted-foreground text-xs">{label}</dt>
              <dd className="flex items-center gap-1 font-mono text-xs break-all">
                <span className="min-w-0">{value}</span>
                {copy && <CopyButton value={copy} label={`Copy ${label.toLowerCase()}`} />}
              </dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}

function PeersCard({ instance, peers, error, reload }: {
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
      if (err instanceof ApiError && err.code === "apply_failed") toast.warning(err.message)
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
            if (!(err instanceof ApiError && err.code === "apply_failed")) throw err
            toast.warning(err.message)
          }
          void reload()
        }}
      />
    </Card>
  )
}
