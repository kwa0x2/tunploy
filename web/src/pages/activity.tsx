import { useCallback, useState } from "react"
import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import {
  Globe,
  KeyRound,
  Pencil,
  Plug,
  PlugZap,
  Play,
  Plus,
  RotateCw,
  Settings,
  ShieldAlert,
  ShieldCheck,
  ShieldOff,
  Square,
  Trash2,
  TriangleAlert,
  Unplug,
} from "lucide-react"
import type { LucideIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { PageHeader } from "@/components/page-header"
import { GeoAttribution, Location } from "@/components/status"
import { useNow } from "@/hooks/use-now"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import type { ActivityEvent, EventCategory } from "@/lib/api"
import { errorMessage, formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"

const pageSize = 50
const maxLimit = 500

const filters: { value?: EventCategory; label: string }[] = [
  { label: "All" },
  { value: "connection", label: "Connections" },
  { value: "change", label: "Changes" },
  { value: "auth", label: "Sign-ins" },
]

export function ActivityPage() {
  const [category, setCategory] = useState<EventCategory>()
  const [limit, setLimit] = useState(pageSize)
  const { data, error } = useResource(
    useCallback(() => api.events({ limit, category }), [limit, category]),
    10_000,
  )
  const now = useNow()

  return (
    <>
      <PageHeader
        title="Activity"
        description="Who connected from where, and what changed on the panel. Kept for 90 days."
      />

      <div className="mb-4 flex flex-wrap gap-1.5">
        {filters.map((f) => (
          <Button
            key={f.label}
            size="sm"
            variant={category === f.value ? "default" : "outline"}
            onClick={() => {
              setCategory(f.value)
              setLimit(pageSize)
            }}
          >
            {f.label}
          </Button>
        ))}
      </div>

      {!data && error !== undefined && (
        <Alert variant="destructive">
          <AlertDescription>{errorMessage(error)}</AlertDescription>
        </Alert>
      )}
      {!data && error === undefined && <Skeleton className="h-64 rounded-xl" />}

      {data && (
        <Card>
          <CardContent>
            {data.length === 0 ? (
              <p className="text-muted-foreground py-8 text-center text-sm">Nothing here yet.</p>
            ) : (
              <ol className="divide-y">
                {data.map((event) => (
                  <EventRow key={event.id} event={event} now={now} />
                ))}
              </ol>
            )}
            {data.length === limit && limit < maxLimit && (
              <div className="flex justify-center pt-4">
                <Button variant="outline" size="sm" onClick={() => setLimit((l) => l + pageSize)}>
                  Load more
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <GeoAttribution className="mt-4" />
    </>
  )
}

type Tone = "good" | "bad" | "neutral"

const tones: Record<Tone, string> = {
  good: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  bad: "bg-red-500/10 text-red-600 dark:text-red-400",
  neutral: "bg-muted text-muted-foreground",
}

function EventRow({ event, now }: { event: ActivityEvent; now: number }) {
  const { icon: Icon, tone, text } = describe(event)
  const at = new Date(event.created_at)

  return (
    <li className="flex flex-col gap-2 py-3 sm:flex-row sm:items-center sm:gap-4">
      <div className="flex min-w-0 flex-1 items-center gap-3">
        <span className={cn("grid size-8 shrink-0 place-items-center rounded-full", tones[tone])}>
          <Icon className="size-4" />
        </span>
        <div className="min-w-0">
          <p className="truncate text-sm">{text}</p>
          {event.detail && (
            <p className="text-muted-foreground truncate text-xs" title={event.detail}>
              {event.detail}
            </p>
          )}
        </div>
      </div>
      <div className="flex items-center justify-between gap-4 pl-11 sm:pl-0">
        {event.ip && <Location ip={event.ip} country={event.country} className="max-w-64" />}
        <time
          dateTime={event.created_at}
          title={at.toLocaleString()}
          className="text-muted-foreground shrink-0 text-xs tabular-nums sm:w-16 sm:text-right"
        >
          {formatRelative(event.created_at, now)}
        </time>
      </div>
    </li>
  )
}

function describe(e: ActivityEvent): { icon: LucideIcon; tone: Tone; text: ReactNode } {
  const server = e.instance_name && <ServerName event={e} />
  const device = e.peer_name && <b className="font-medium">{e.peer_name}</b>

  switch (e.kind) {
    case "device.connected":
      return { icon: PlugZap, tone: "good", text: <>{device} connected to {server}</> }
    case "device.disconnected":
      return { icon: Unplug, tone: "neutral", text: <>{device} disconnected from {server}</> }
    case "device.created":
      return { icon: Plus, tone: "neutral", text: <>{device} added to {server}</> }
    case "device.deleted":
      return { icon: Trash2, tone: "neutral", text: <>{device} removed from {server}</> }
    case "device.enabled":
      return { icon: Plug, tone: "neutral", text: <>{device} enabled on {server}</> }
    case "device.disabled":
      return { icon: Unplug, tone: "neutral", text: <>{device} disabled on {server}</> }
    case "device.renamed":
      return { icon: Pencil, tone: "neutral", text: <>Device renamed to {device} on {server}</> }
    case "server.created":
      return { icon: Plus, tone: "good", text: <>Server {server} created</> }
    case "server.deploy_failed":
      return { icon: TriangleAlert, tone: "bad", text: <>Deploying {server} failed</> }
    case "server.updated":
      return { icon: Pencil, tone: "neutral", text: <>Server {server} settings changed</> }
    case "server.deleted":
      return { icon: Trash2, tone: "neutral", text: <>Server {server} deleted</> }
    case "server.started":
      return { icon: Play, tone: "neutral", text: <>Server {server} started</> }
    case "server.stopped":
      return { icon: Square, tone: "neutral", text: <>Server {server} stopped</> }
    case "server.restarted":
      return { icon: RotateCw, tone: "neutral", text: <>Server {server} restarted</> }
    case "settings.updated":
      return { icon: Settings, tone: "neutral", text: "Panel settings changed" }
    case "settings.domain_changed":
      return { icon: Globe, tone: "neutral", text: e.detail ? "Panel domain set" : "Panel domain removed" }
    case "auth.login":
      return { icon: KeyRound, tone: "good", text: "Signed in" }
    case "auth.login_failed":
      return { icon: ShieldAlert, tone: "bad", text: "Failed sign-in attempt" }
    case "auth.password_changed":
      return { icon: KeyRound, tone: "neutral", text: "Password changed" }
    case "auth.totp_enabled":
      return { icon: ShieldCheck, tone: "good", text: "Two-factor authentication turned on" }
    case "auth.totp_disabled":
      return { icon: ShieldOff, tone: "bad", text: "Two-factor authentication turned off" }
    default:
      return { icon: Settings, tone: "neutral", text: e.kind }
  }
}

function ServerName({ event }: { event: ActivityEvent }) {
  const deleted = event.kind === "server.deleted" || event.kind === "server.deploy_failed"
  if (deleted || !event.instance_id) return <b className="font-medium">{event.instance_name}</b>
  return (
    <Link to={`/servers/${event.instance_id}`} className="font-medium underline-offset-4 hover:underline">
      {event.instance_name}
    </Link>
  )
}
