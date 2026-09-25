import { Link } from "react-router-dom"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { PageHeader } from "@/components/page-header"
import { DeviceIcon, GeoAttribution, Location, MonthUsage, PeerStatus } from "@/components/status"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { loadFleet } from "@/lib/fleet"
import { endpointHost, errorMessage, formatRelative, isOnline, lastSeen } from "@/lib/format"

export function PeersPage() {
  const { data, error } = useResource(loadFleet, 10_000)
  const now = useNow()

  return (
    <>
      <PageHeader title="Peers" description="Every device allowed onto your VPN, across all servers." />

      {!data && error !== undefined && (
        <Alert variant="destructive">
          <AlertDescription>{errorMessage(error)}</AlertDescription>
        </Alert>
      )}
      {!data && error === undefined && <Skeleton className="h-48 rounded-xl" />}

      {data && (
        <Card>
          <CardContent>
            {data.peers.length === 0 ? (
              <p className="text-muted-foreground py-8 text-center text-sm">
                No peers yet.{" "}
                {data.instances.length === 0 ? (
                  <>
                    <Link to="/servers" className="text-foreground underline underline-offset-4">
                      Create a server
                    </Link>{" "}
                    first.
                  </>
                ) : (
                  "Open a server to add one."
                )}
              </p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Server</TableHead>
                    <TableHead>Address</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Location</TableHead>
                    <TableHead>This month</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.peers.map((peer) => {
                    const handshake = lastSeen(peer)
                    return (
                      <TableRow key={peer.id}>
                        <TableCell className="max-w-48 font-medium">
                          <span className="flex items-center gap-2.5">
                            <DeviceIcon />
                            <span className="truncate">{peer.name}</span>
                          </span>
                        </TableCell>
                        <TableCell>
                          <Link
                            to={`/servers/${peer.instance.id}`}
                            className="hover:text-foreground text-muted-foreground underline-offset-4 hover:underline"
                          >
                            {peer.instance.name}
                          </Link>
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
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}
      {data && data.peers.length > 0 && <GeoAttribution className="mt-4" />}
    </>
  )
}
