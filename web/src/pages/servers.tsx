import { useMemo, useState } from "react"
import { Link } from "react-router-dom"
import { Plus, Server } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { InstanceStatus } from "@/components/status"
import { EmptyServers, NewServerDialog } from "@/components/new-server"
import { PageHeader } from "@/components/page-header"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import type { Instance, Node } from "@/lib/api"
import { endpointOf, errorMessage } from "@/lib/format"

export function ServersPage() {
  const { data: instances, error } = useResource(api.instances, 10_000)
  const { data: nodes } = useResource(api.nodes, 30_000)
  const [creating, setCreating] = useState(false)
  // Only worth naming once there is more than one machine.
  const nodeOf = useMemo(() => {
    const byId = new Map((nodes ?? []).map((n) => [n.id, n]))
    return (id: number) => (byId.size > 1 ? byId.get(id) : undefined)
  }, [nodes])

  return (
    <>
      <PageHeader
        title="Servers"
        description="WireGuard servers on this machine and your other nodes."
        actions={
          instances?.length ? (
            <Button onClick={() => setCreating(true)}>
              <Plus />
              New server
            </Button>
          ) : null
        }
      />

      {error !== undefined && !instances && (
        <Alert variant="destructive">
          <AlertDescription>{errorMessage(error)}</AlertDescription>
        </Alert>
      )}

      {!instances && error === undefined && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-36 rounded-xl" />
          ))}
        </div>
      )}

      {instances?.length === 0 && <EmptyServers onCreate={() => setCreating(true)} />}

      {instances && instances.length > 0 && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {instances.map((instance) => (
            <ServerCard key={instance.id} instance={instance} node={nodeOf(instance.node_id)} />
          ))}
        </div>
      )}

      <NewServerDialog open={creating} onOpenChange={setCreating} />
    </>
  )
}

function ServerCard({ instance, node }: { instance: Instance; node?: Node }) {
  return (
    <Link to={`/servers/${instance.id}`} className="group rounded-xl outline-none">
      <Card className="group-hover:ring-primary/30 group-focus-visible:ring-ring h-full transition-all group-hover:-translate-y-0.5 group-hover:shadow-lg group-hover:shadow-amber-500/10 motion-reduce:transform-none">
        <CardHeader className="flex flex-row items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-3">
            <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-yellow-400/20 text-yellow-700 dark:text-yellow-300">
              <Server className="size-4" />
            </div>
            <CardTitle className="truncate">{instance.name}</CardTitle>
          </div>
          <InstanceStatus state={instance.status.state} />
        </CardHeader>
        <CardContent className="text-muted-foreground grid grid-cols-2 gap-y-1 text-sm">
          {node && (
            <>
              <span>Node</span>
              <span className="text-foreground truncate text-right">{node.name}</span>
            </>
          )}
          <span>Endpoint</span>
          <span className="text-foreground truncate text-right font-mono text-xs leading-5">
            {endpointOf(instance)}
          </span>
          <span>Subnet</span>
          <span className="text-foreground text-right font-mono text-xs leading-5">
            {instance.address}
          </span>
          <span>Peers</span>
          <span className="text-foreground text-right tabular-nums">{instance.peer_count}</span>
        </CardContent>
      </Card>
    </Link>
  )
}
