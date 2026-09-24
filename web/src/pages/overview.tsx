import { useState } from "react"
import type { ComponentType, ReactNode } from "react"
import { Link } from "react-router-dom"
import { ArrowDownUp, ArrowRight, Plus, QrCode, Server, Smartphone, Wifi } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { NewServerDialog } from "@/components/new-server"
import { DeviceIcon, InstanceStatus, PeerStatus } from "@/components/status"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import type { Instance } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { loadFleet } from "@/lib/fleet"
import type { Fleet } from "@/lib/fleet"
import { endpointOf, formatBytes, formatRelative, isOnline } from "@/lib/format"
import { cn } from "@/lib/utils"

type FleetPeer = Fleet["peers"][number]

const brandTone = "bg-yellow-400/20 text-yellow-700 dark:text-yellow-300"

const totalBytes = (p: FleetPeer) => (p.stats ? p.stats.rx_bytes + p.stats.tx_bytes : 0)

export function OverviewPage() {
  const fleet = useResource(loadFleet, 10_000)
  const docker = useResource(api.dockerStatus, 30_000)
  const [creating, setCreating] = useState(false)
  const now = useNow()

  const data = fleet.data
  const online = data?.peers.filter((p) => isOnline(p)) ?? []
  const enabled = data?.peers.filter((p) => p.enabled) ?? []
  const running = data?.instances.filter((i) => i.status.state === "running") ?? []
  // Devices download what the server sends them, so tx is their download.
  const down = data?.peers.reduce((sum, p) => sum + (p.stats?.tx_bytes ?? 0), 0) ?? 0
  const up = data?.peers.reduce((sum, p) => sum + (p.stats?.rx_bytes ?? 0), 0) ?? 0

  return (
    <>
      <Greeting
        summary={
          data &&
          (data.instances.length === 0
            ? "Let's get your first VPN server running."
            : `${running.length} of ${data.instances.length} ${plural(data.instances.length, "server")} running · ${online.length} ${plural(online.length, "device")} online right now.`)
        }
        // The getting-started card has its own button; one is enough.
        onCreate={data?.instances.length ? () => setCreating(true) : undefined}
      />

      {docker.data && !docker.data.available && (
        <Alert variant="destructive" className="mb-6">
          <AlertTitle>Docker is not reachable</AlertTitle>
          <AlertDescription>
            Tunploy runs every VPN server as a container, so nothing can be deployed or managed
            until this is fixed: {docker.data.error}
          </AlertDescription>
        </Alert>
      )}

      {data?.instances.length !== 0 && (
        <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
          <StatTile
            label="VPN servers"
            value={data?.instances.length}
            detail={`${running.length} running`}
            icon={Server}
            tone={brandTone}
          />
          <StatTile
            label="Devices"
            value={data?.peers.length}
            detail={`${enabled.length} enabled`}
            icon={Smartphone}
            tone="bg-sky-500/10 text-sky-600 dark:text-sky-400"
          />
          <StatTile
            label="Online now"
            value={data && online.length}
            detail={enabled.length ? `of ${enabled.length} enabled ${plural(enabled.length, "device")}` : "No devices yet"}
            icon={Wifi}
            tone="bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
          />
          <StatTile
            label="Traffic"
            value={data && formatBytes(down + up)}
            detail={`↓ ${formatBytes(down)} · ↑ ${formatBytes(up)}`}
            icon={ArrowDownUp}
            tone="bg-violet-500/10 text-violet-600 dark:text-violet-400"
          />
        </div>
      )}

      <div className={cn(data?.instances.length !== 0 && "mt-6")}>
        {!data && <Skeleton className="h-64 rounded-xl" />}
        {data?.instances.length === 0 && <GetStarted onCreate={() => setCreating(true)} />}
        {data && data.instances.length > 0 && (
          <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
            <ServersCard instances={data.instances} peers={data.peers} />
            <div className="grid content-start gap-6">
              <TopTrafficCard peers={data.peers} />
              <RecentCard peers={data.peers} now={now} />
            </div>
          </div>
        )}
      </div>

      <NewServerDialog open={creating} onOpenChange={setCreating} />
    </>
  )
}

function plural(n: number, word: string) {
  return n === 1 ? word : `${word}s`
}

function greeting(hour: number) {
  if (hour < 5) return "Good night"
  if (hour < 12) return "Good morning"
  if (hour < 18) return "Good afternoon"
  return "Good evening"
}

function Greeting({ summary, onCreate }: { summary?: ReactNode; onCreate?: () => void }) {
  const { user } = useAuth()
  const firstName = user?.name.split(/\s+/)[0]

  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight md:text-3xl">
          {greeting(new Date().getHours())}
          {firstName ? `, ${firstName}` : ""}
        </h1>
        <p className="text-muted-foreground text-sm md:text-base">{summary ?? " "}</p>
      </div>
      {onCreate && (
        <Button onClick={onCreate}>
          <Plus />
          New server
        </Button>
      )}
    </div>
  )
}

function StatTile({ label, value, detail, icon: Icon, tone }: {
  label: string
  value?: ReactNode
  detail: string
  icon: ComponentType<{ className?: string }>
  tone: string
}) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-start sm:gap-4">
        <div className={cn("grid size-10 shrink-0 place-items-center rounded-xl sm:size-11", tone)}>
          <Icon className="size-5" />
        </div>
        <div className="min-w-0 space-y-0.5">
          <p className="text-muted-foreground text-sm">{label}</p>
          {value === undefined ? (
            <Skeleton className="h-8 w-16" />
          ) : (
            <p className="text-2xl font-semibold tracking-tight tabular-nums">{value}</p>
          )}
          <p className="text-muted-foreground truncate text-xs">{value === undefined ? " " : detail}</p>
        </div>
      </CardContent>
    </Card>
  )
}

function ServersCard({ instances, peers }: { instances: Instance[]; peers: FleetPeer[] }) {
  return (
    <Card className="self-start">
      <CardHeader>
        <CardTitle>Servers</CardTitle>
        <CardDescription>Devices online out of those enabled, per server.</CardDescription>
        <CardAction>
          <Button variant="ghost" size="sm" nativeButton={false} render={<Link to="/servers" />}>
            View all
            <ArrowRight />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-2">
        {instances.map((instance) => {
          const own = peers.filter((p) => p.instance_id === instance.id)
          const enabled = own.filter((p) => p.enabled).length
          const online = own.filter((p) => isOnline(p)).length
          const traffic = own.reduce((sum, p) => sum + totalBytes(p), 0)
          return (
            <Link
              key={instance.id}
              to={`/servers/${instance.id}`}
              className="hover:bg-accent/60 focus-visible:ring-ring -mx-2 block rounded-lg px-2 py-3 outline-none focus-visible:ring-2"
            >
              <div className="flex items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-3">
                  <div className={cn("grid size-9 shrink-0 place-items-center rounded-lg", brandTone)}>
                    <Server className="size-4" />
                  </div>
                  <div className="min-w-0">
                    <p className="truncate font-medium">{instance.name}</p>
                    <p className="text-muted-foreground truncate font-mono text-xs">{endpointOf(instance)}</p>
                  </div>
                </div>
                <InstanceStatus state={instance.status.state} />
              </div>
              <div className="mt-3 flex items-center gap-3 pl-12">
                <Meter value={online} max={enabled} label={`${online} of ${enabled} enabled devices online`} />
                <span className="text-muted-foreground w-20 shrink-0 text-right text-xs tabular-nums">
                  {online}/{enabled} online
                </span>
                <span className="text-muted-foreground hidden w-16 shrink-0 text-right text-xs tabular-nums sm:inline">
                  {formatBytes(traffic)}
                </span>
              </div>
            </Link>
          )
        })}
      </CardContent>
    </Card>
  )
}

function Meter({ value, max, label }: { value: number; max: number; label: string }) {
  const pct = max > 0 ? (value / max) * 100 : 0
  return (
    <div
      role="meter"
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
      className="bg-muted h-2 flex-1 overflow-hidden rounded-full"
    >
      <div className="h-full rounded-full bg-emerald-500 transition-[width]" style={{ width: `${pct}%` }} />
    </div>
  )
}

function TopTrafficCard({ peers }: { peers: FleetPeer[] }) {
  const top = peers
    .filter((p) => totalBytes(p) > 0)
    .sort((a, b) => totalBytes(b) - totalBytes(a))
    .slice(0, 5)
  const max = top.length ? totalBytes(top[0]) : 0

  return (
    <Card>
      <CardHeader>
        <CardTitle>Top devices by traffic</CardTitle>
        <CardDescription>Since each server last started.</CardDescription>
      </CardHeader>
      <CardContent>
        {top.length === 0 ? (
          <p className="text-muted-foreground py-4 text-center text-sm">No traffic yet.</p>
        ) : (
          <ol className="space-y-3">
            {top.map((peer) => (
              <li
                key={peer.id}
                title={`↓ ${formatBytes(peer.stats?.tx_bytes ?? 0)} downloaded · ↑ ${formatBytes(peer.stats?.rx_bytes ?? 0)} uploaded`}
              >
                <div className="mb-1 flex items-baseline justify-between gap-3 text-sm">
                  <span className="min-w-0 truncate">
                    <span className="font-medium">{peer.name}</span>
                    <span className="text-muted-foreground"> · {peer.instance.name}</span>
                  </span>
                  <span className="shrink-0 tabular-nums">{formatBytes(totalBytes(peer))}</span>
                </div>
                <div className="bg-muted h-2 overflow-hidden rounded-full">
                  <div
                    className="bg-chart-1 h-full rounded-full"
                    style={{ width: `${Math.max(2, (totalBytes(peer) / max) * 100)}%` }}
                  />
                </div>
              </li>
            ))}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}

function RecentCard({ peers, now }: { peers: FleetPeer[]; now: number }) {
  const recent = peers
    .filter((p) => p.stats?.latest_handshake)
    .sort((a, b) => b.stats!.latest_handshake!.localeCompare(a.stats!.latest_handshake!))
    .slice(0, 5)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Recently active</CardTitle>
      </CardHeader>
      <CardContent>
        {recent.length === 0 ? (
          <p className="text-muted-foreground py-4 text-center text-sm">
            No device has connected yet.
          </p>
        ) : (
          <ul className="space-y-3">
            {recent.map((peer) => (
              <li key={peer.id} className="flex items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-3">
                  <DeviceIcon className="size-8" />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{peer.name}</p>
                    <p className="text-muted-foreground truncate text-xs">{peer.instance.name}</p>
                  </div>
                </div>
                <PeerStatus
                  enabled={peer.enabled}
                  online={isOnline(peer)}
                  lastSeen={formatRelative(peer.stats!.latest_handshake!, now)}
                />
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

const steps = [
  {
    icon: Server,
    title: "Deploy a server",
    text: "One click. Subnet, port and keys are picked for you.",
    tone: brandTone,
  },
  {
    icon: Smartphone,
    title: "Add a device",
    text: "Each phone or laptop gets its own address and keys.",
    tone: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
  },
  {
    icon: QrCode,
    title: "Scan and connect",
    text: "Scan the QR code with the WireGuard app. That's it.",
    tone: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  },
]

function GetStarted({ onCreate }: { onCreate: () => void }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Get started in three steps</CardTitle>
        <CardDescription>From zero to a working VPN in about a minute.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <ol className="grid gap-4 md:grid-cols-3">
          {steps.map(({ icon: Icon, title, text, tone }, i) => (
            <li key={title} className="bg-muted/40 rounded-xl border p-4">
              <div className="mb-3 flex items-center gap-3">
                <div className={cn("grid size-10 place-items-center rounded-xl", tone)}>
                  <Icon className="size-5" />
                </div>
                <span className="text-muted-foreground text-xs font-medium">Step {i + 1}</span>
              </div>
              <p className="font-medium">{title}</p>
              <p className="text-muted-foreground mt-1 text-sm">{text}</p>
            </li>
          ))}
        </ol>
        <Button onClick={onCreate}>
          <Plus />
          Create your first server
        </Button>
      </CardContent>
    </Card>
  )
}
