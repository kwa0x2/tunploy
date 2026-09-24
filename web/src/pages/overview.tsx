import { Activity, Server, Users } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { PageHeader } from "@/components/page-header"

const stats = [
  { label: "VPN servers", value: "0", icon: Server },
  { label: "Peers", value: "0", icon: Users },
  { label: "Handshakes (24h)", value: "0", icon: Activity },
]

export function OverviewPage() {
  return (
    <>
      <PageHeader
        title="Overview"
        description="Everything running on this node, at a glance."
      />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {stats.map(({ label, value, icon: Icon }) => (
          <Card key={label}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-muted-foreground text-sm font-medium">{label}</CardTitle>
              <Icon className="text-muted-foreground size-4" />
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-semibold tabular-nums">{value}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>No VPN server yet</CardTitle>
        </CardHeader>
        <CardContent className="text-muted-foreground text-sm">
          Once WireGuard deployment lands, this is where you will spin up your first server in one
          click.
        </CardContent>
      </Card>
    </>
  )
}
