import { useCallback, useState } from "react"
import type { ReactNode } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import {
  ArrowLeft,
  HardDrive,
  Loader2,
  MoreHorizontal,
  Play,
  RotateCw,
  ScrollText,
  Settings2,
  Square,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { InstanceStatus } from "@/components/status"
import { InstanceFormDialog } from "@/features/servers/instance-form-dialog"
import { LogsDialog } from "@/features/servers/logs-dialog"
import { PeersCard } from "@/features/peers/peers-card"
import { ConnectionCard } from "@/features/servers/connection-card"
import { useResource } from "@/hooks/use-resource"
import { ApiError, api } from "@/api"
import { endpointOf, errorMessage, locationText } from "@/lib/format"

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
