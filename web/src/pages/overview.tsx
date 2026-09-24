import { useState } from "react"
import { Link } from "react-router-dom"
import { Activity, Server, Users } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyServers, NewServerDialog } from "@/components/new-server"
import { PageHeader } from "@/components/page-header"
import { InstanceStatus } from "@/components/status"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import { loadFleet } from "@/lib/fleet"
import { endpointOf, isOnline } from "@/lib/format"

export function OverviewPage() {
  const fleet = useResource(loadFleet, 10_000)
  const docker = useResource(api.dockerStatus, 30_000)
  const [creating, setCreating] = useState(false)

  const data = fleet.data
  const now = useNow()
  const stats = [
    {
      label: "VPN servers",
      value: data?.instances.length,
      detail: data && `${data.instances.filter((i) => i.status.state === "running").length} running`,
      icon: Server,
    },
    {
      label: "Peers",
      value: data?.peers.length,
      detail: data && `${data.peers.filter((p) => p.enabled).length} enabled`,
      icon: Users,
    },
    {
      label: "Online now",
      value: data?.peers.filter((p) => isOnline(p, now)).length,
      detail: "Handshake in the last 3 minutes",
      icon: Activity,
    },
  ]

  return (
    <>
      <PageHeader title="Overview" description="Everything running on this node, at a glance." />

      {docker.data && !docker.data.available && (
        <Alert variant="destructive" className="mb-6">
          <AlertTitle>Docker is not reachable</AlertTitle>
          <AlertDescription>
            Tunploy runs every VPN server as a container, so nothing can be deployed or managed
            until this is fixed: {docker.data.error}
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {stats.map(({ label, value, detail, icon: Icon }) => (
          <Card key={label}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-muted-foreground text-sm font-medium">{label}</CardTitle>
              <Icon className="text-muted-foreground size-4" />
            </CardHeader>
            <CardContent className="space-y-1">
              {value === undefined ? (
                <Skeleton className="h-9 w-12" />
              ) : (
                <div className="text-3xl font-semibold tabular-nums">{value}</div>
              )}
              <p className="text-muted-foreground text-xs">{detail ?? " "}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="mt-6">
        {data?.instances.length === 0 && <EmptyServers onCreate={() => setCreating(true)} />}
        {data && data.instances.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Servers</CardTitle>
            </CardHeader>
            <CardContent className="divide-y">
              {data.instances.map((instance) => (
                <Link
                  key={instance.id}
                  to={`/servers/${instance.id}`}
                  className="hover:bg-muted/50 -mx-2 flex items-center justify-between gap-4 rounded-md px-2 py-3"
                >
                  <div className="min-w-0">
                    <p className="truncate font-medium">{instance.name}</p>
                    <p className="text-muted-foreground font-mono text-xs">{endpointOf(instance)}</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-4">
                    <span className="text-muted-foreground text-sm tabular-nums">
                      {instance.peer_count} {instance.peer_count === 1 ? "peer" : "peers"}
                    </span>
                    <InstanceStatus state={instance.status.state} />
                  </div>
                </Link>
              ))}
            </CardContent>
          </Card>
        )}
      </div>

      <NewServerDialog open={creating} onOpenChange={setCreating} />
    </>
  )
}
