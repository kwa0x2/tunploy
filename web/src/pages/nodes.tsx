import { useState } from "react"
import { HardDrive, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { AddNodeDialog, RenameNodeDialog } from "@/components/node-dialogs"
import { PageHeader } from "@/components/page-header"
import { NodeStatus } from "@/components/status"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import type { Node } from "@/lib/api"
import { errorMessage, formatBytes, formatRelative } from "@/lib/format"

export function NodesPage() {
  const { data: nodes, error, reload } = useResource(api.nodes, 5_000)
  const [adding, setAdding] = useState(false)
  const [renaming, setRenaming] = useState<Node>()
  const [deleting, setDeleting] = useState<Node>()

  return (
    <>
      <PageHeader
        title="Nodes"
        description="The machines your VPN servers run on. The panel reaches each one over SSH."
        actions={
          <Button onClick={() => setAdding(true)}>
            <Plus />
            Add node
          </Button>
        }
      />

      {error !== undefined && !nodes && (
        <Alert variant="destructive">
          <AlertDescription>{errorMessage(error)}</AlertDescription>
        </Alert>
      )}

      {!nodes && error === undefined && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-48 rounded-xl" />
          ))}
        </div>
      )}

      {nodes && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {nodes.map((node) => (
            <NodeCard
              key={node.id}
              node={node}
              onRename={() => setRenaming(node)}
              onDelete={() => setDeleting(node)}
            />
          ))}
          {nodes.length === 1 && <AddNodeCard onAdd={() => setAdding(true)} />}
        </div>
      )}

      <AddNodeDialog
        open={adding}
        onOpenChange={setAdding}
        onAdded={(node) => {
          toast.success(`${node.name} is ready for VPN servers.`)
          void reload()
        }}
      />
      <RenameNodeDialog
        node={renaming}
        onOpenChange={(open) => !open && setRenaming(undefined)}
        onSaved={() => void reload()}
      />
      {deleting && (
        <DeleteNodeDialog
          node={deleting}
          onOpenChange={(open) => !open && setDeleting(undefined)}
          onDeleted={() => void reload()}
        />
      )}
    </>
  )
}

function NodeCard({ node, onRename, onDelete }: { node: Node; onRename: () => void; onDelete: () => void }) {
  const now = useNow(30_000)
  const d = node.status.daemon
  const offline = node.status.state === "offline"

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-sky-500/10 text-sky-600 dark:text-sky-400">
            <HardDrive className="size-4" />
          </div>
          <div className="min-w-0">
            <CardTitle className="truncate">{node.name}</CardTitle>
            <p className="text-muted-foreground truncate font-mono text-xs">
              {node.local ? node.host || "panel's own machine" : `${node.username}@${node.host}:${node.port}`}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <NodeStatus state={node.status.state} />
          {!node.local && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="ghost" size="icon-sm" aria-label={`Actions for ${node.name}`} />}
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-40">
                <DropdownMenuItem onClick={onRename}>
                  <Pencil />
                  Rename
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={onDelete}>
                  <Trash2 />
                  Remove node
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {offline && node.status.error && (
          <p className="rounded-md bg-red-500/10 px-2.5 py-2 text-xs text-red-700 dark:text-red-400">
            {node.status.error}
            {node.last_seen_at && <> · last seen {formatRelative(node.last_seen_at, now)}</>}
          </p>
        )}
        <dl className="text-muted-foreground grid grid-cols-2 gap-y-1 text-sm">
          <dt>Servers</dt>
          <dd className="text-foreground text-right tabular-nums">{node.server_count}</dd>
          {d && (
            <>
              <dt>System</dt>
              <dd className="text-foreground truncate text-right text-xs leading-5" title={d.os}>
                {d.os}
              </dd>
              <dt>Kernel</dt>
              <dd className="text-foreground truncate text-right font-mono text-xs leading-5">
                {d.kernel_version}
              </dd>
              <dt>Resources</dt>
              <dd className="text-foreground text-right text-xs leading-5 tabular-nums">
                {d.cpus} CPU · {formatBytes(d.memory)}
              </dd>
              <dt>Docker</dt>
              <dd className="text-foreground text-right font-mono text-xs leading-5">{d.version}</dd>
            </>
          )}
          {!node.local && node.host_key_fingerprint && (
            <>
              <dt>Host key</dt>
              <dd
                className="text-foreground truncate text-right font-mono text-xs leading-5"
                title={node.host_key_fingerprint}
              >
                {node.host_key_fingerprint.replace("SHA256:", "").slice(0, 16)}…
              </dd>
            </>
          )}
        </dl>
      </CardContent>
    </Card>
  )
}

function AddNodeCard({ onAdd }: { onAdd: () => void }) {
  return (
    <button
      type="button"
      onClick={onAdd}
      className="text-muted-foreground hover:border-primary/50 hover:text-foreground focus-visible:ring-ring flex min-h-48 flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed p-6 text-center text-sm transition-colors outline-none focus-visible:ring-3"
    >
      <Plus className="size-5" />
      <span className="font-medium">Add another server</span>
      <span className="max-w-60 text-xs">
        Any Linux VPS you can SSH into. Docker is installed for you if it is missing.
      </span>
    </button>
  )
}

function DeleteNodeDialog({ node, onOpenChange, onDeleted }: {
  node: Node
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const offline = node.status.state !== "online"
  const servers = node.server_count === 1 ? "1 server" : `${node.server_count} servers`

  return (
    <ConfirmDialog
      open
      onOpenChange={onOpenChange}
      title={`Remove ${node.name}?`}
      description={
        offline
          ? `${node.name} is offline, so the panel cannot clean it up. Its ${servers} and their devices are deleted from the panel, but anything already running there keeps running until you remove it by hand.`
          : `Its ${servers} and their devices are deleted, the VPN containers and /var/lib/tunploy are removed from the machine, and the panel's SSH key is taken out of authorized_keys. Docker stays installed.`
      }
      confirmLabel={offline ? "Forget node" : "Remove node"}
      onConfirm={async () => {
        await api.deleteNode(node.id, offline)
        toast.success(`${node.name} was removed.`)
        onDeleted()
      }}
    />
  )
}
