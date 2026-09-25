import { useCallback, useState } from "react"
import type { ReactNode } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { UsageMeter } from "@/components/status"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import type { Peer, UsagePoint } from "@/lib/api"
import { errorMessage, formatBytes, formatExpiry, monthTotal, nextMonthStart } from "@/lib/format"
import { cn } from "@/lib/utils"

interface Props {
  peer?: Peer
  open: boolean
  onOpenChange: (open: boolean) => void
  onEditLimits: (peer: Peer) => void
}

export function PeerUsageDialog({ peer, open, onOpenChange, onEditLimits }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{peer?.name}</DialogTitle>
          <DialogDescription>Data used through the tunnel, downloads and uploads together.</DialogDescription>
        </DialogHeader>
        {peer && <PeerUsageBody key={peer.id} peer={peer} onEditLimits={() => onEditLimits(peer)} />}
      </DialogContent>
    </Dialog>
  )
}

type Period = "daily" | "monthly"

function PeerUsageBody({ peer, onEditLimits }: { peer: Peer; onEditLimits: () => void }) {
  const [period, setPeriod] = useState<Period>("daily")
  const usage = useResource(
    useCallback(() => api.peerUsage(peer.instance_id, peer.id), [peer.instance_id, peer.id]),
    30_000,
  )
  const used = monthTotal(peer)
  const resets = nextMonthStart().toLocaleDateString(undefined, { day: "numeric", month: "short" })

  return (
    <div className="min-w-0 space-y-5">
      <dl className="grid gap-3 sm:grid-cols-3">
        <Figure label="This month">
          <dd className="text-xl font-semibold tracking-tight">{formatBytes(used)}</dd>
          <dd className="text-muted-foreground text-xs tabular-nums">
            ↓ {formatBytes(peer.month_usage.tx_bytes)} · ↑ {formatBytes(peer.month_usage.rx_bytes)}
          </dd>
        </Figure>
        <Figure label="Monthly limit" action={<EditLink onClick={onEditLimits} />}>
          {peer.data_limit ? (
            <>
              <dd className="text-xl font-semibold tracking-tight">{formatBytes(peer.data_limit)}</dd>
              <dd className="space-y-1.5">
                <UsageMeter used={used} limit={peer.data_limit} className="mt-1" />
                <p className="text-muted-foreground text-xs">
                  {used >= peer.data_limit
                    ? `Used up, back on ${resets}`
                    : `${formatBytes(peer.data_limit - used)} left · resets ${resets}`}
                </p>
              </dd>
            </>
          ) : (
            <dd className="text-muted-foreground text-sm">No limit</dd>
          )}
        </Figure>
        <Figure label="Access" action={<EditLink onClick={onEditLimits} />}>
          {peer.expires_at ? (
            <dd className={cn("text-sm font-medium", peer.blocked === "expired" && "text-red-700 dark:text-red-400")}>
              {peer.blocked === "expired" ? "Ended at" : "Until"} {formatExpiry(peer.expires_at)}
            </dd>
          ) : (
            <dd className="text-muted-foreground text-sm">No end date</dd>
          )}
        </Figure>
      </dl>

      <div className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm font-medium">{period === "daily" ? "Last 30 days" : "Last 12 months"}</p>
          <div className="flex gap-1.5">
            {(["daily", "monthly"] as const).map((p) => (
              <Button
                key={p}
                size="xs"
                variant={period === p ? "secondary" : "outline"}
                onClick={() => setPeriod(p)}
              >
                {p === "daily" ? "Days" : "Months"}
              </Button>
            ))}
          </div>
        </div>
        {!usage.data && usage.error !== undefined && (
          <Alert variant="destructive">
            <AlertDescription>{errorMessage(usage.error)}</AlertDescription>
          </Alert>
        )}
        {!usage.data && usage.error === undefined && <Skeleton className="h-48" />}
        {usage.data && <UsageChart points={usage.data[period]} period={period} />}
      </div>
    </div>
  )
}

function Figure({ label, action, children }: { label: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="bg-muted/40 min-w-0 space-y-0.5 rounded-lg border p-3">
      <dt className="text-muted-foreground flex items-center justify-between text-xs">
        {label}
        {action}
      </dt>
      {children}
    </div>
  )
}

function EditLink({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="text-foreground underline-offset-4 hover:underline">
      Edit
    </button>
  )
}

const localDate = (start: string) => {
  const [y, m, d] = start.split("-").map(Number)
  return new Date(y, m - 1, d)
}

function pointLabel(start: string, period: Period, long = false) {
  const date = localDate(start)
  if (period === "monthly") {
    return date.toLocaleDateString(undefined, long ? { month: "long", year: "numeric" } : { month: "short" })
  }
  return date.toLocaleDateString(
    undefined,
    long ? { weekday: "short", day: "numeric", month: "short" } : { day: "numeric", month: "short" },
  )
}

// Round the axis top up to 1, 2 or 5 of a binary unit so tick labels stay short.
function axisMax(max: number) {
  if (max <= 0) return 1024
  let unit = 1
  while (max / unit >= 1024) unit *= 1024
  const v = max / unit
  const magnitude = 10 ** Math.floor(Math.log10(v))
  const step = [1, 2, 5, 10].find((s) => s * magnitude >= v) ?? 10
  return step * magnitude * unit
}

const total = (p: UsagePoint) => p.rx_bytes + p.tx_bytes

function UsageChart({ points, period }: { points: UsagePoint[]; period: Period }) {
  const [hover, setHover] = useState<number>()
  const top = axisMax(Math.max(...points.map(total)))
  const labelEvery = period === "daily" ? 7 : 1

  if (points.every((p) => total(p) === 0)) {
    return (
      <div className="text-muted-foreground grid h-48 place-items-center rounded-lg border border-dashed text-center text-sm">
        No traffic recorded in this period yet.
      </div>
    )
  }

  return (
    <figure>
      <div className="flex h-48 gap-2">
        <div className="text-muted-foreground flex w-12 shrink-0 flex-col justify-between pb-5 text-right text-[10px] tabular-nums">
          <span className="-translate-y-1/2">{formatBytes(top)}</span>
          <span>{formatBytes(top / 2)}</span>
          <span className="translate-y-1/2">0</span>
        </div>
        <div className="relative min-w-0 flex-1">
          <div className="pointer-events-none absolute inset-x-0 top-0 bottom-5 flex flex-col justify-between">
            {[0, 1, 2].map((i) => (
              <div key={i} className="border-border border-t" />
            ))}
          </div>
          <div className="absolute inset-0 flex gap-0.5" onMouseLeave={() => setHover(undefined)}>
            {points.map((p, i) => {
              const pct = (total(p) / top) * 100
              const edge = i < points.length / 4 ? "left" : i >= (points.length * 3) / 4 ? "right" : "center"
              return (
                <div
                  key={p.start}
                  className="relative flex min-w-0 flex-1 flex-col"
                  onMouseEnter={() => setHover(i)}
                >
                  <div className={cn("flex flex-1 items-end justify-center rounded-sm", hover === i && "bg-muted/70")}>
                    {total(p) > 0 && (
                      <div
                        className="bg-chart-1 w-full max-w-6 rounded-t-[4px]"
                        style={{ height: `max(${pct}%, 2px)` }}
                      />
                    )}
                  </div>
                  <div
                    className={cn(
                      "text-muted-foreground flex h-5 justify-center pt-1.5 text-[10px] whitespace-nowrap",
                      i === 0 && "justify-start",
                      i === points.length - 1 && "justify-end",
                    )}
                  >
                    {(points.length - 1 - i) % labelEvery === 0 && (
                      <span className={cn(period === "monthly" && i % 2 === 1 && "max-sm:hidden")}>
                        {pointLabel(p.start, period)}
                      </span>
                    )}
                  </div>
                  {hover === i && <Tooltip point={p} period={period} edge={edge} height={pct} />}
                </div>
              )
            })}
          </div>
        </div>
      </div>
      <table className="sr-only">
        <caption>Data used per {period === "daily" ? "day" : "month"}</caption>
        <thead>
          <tr>
            <th>{period === "daily" ? "Day" : "Month"}</th>
            <th>Downloaded</th>
            <th>Uploaded</th>
          </tr>
        </thead>
        <tbody>
          {points.map((p) => (
            <tr key={p.start}>
              <td>{pointLabel(p.start, period, true)}</td>
              <td>{formatBytes(p.tx_bytes)}</td>
              <td>{formatBytes(p.rx_bytes)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  )
}

// Sits just above its bar, but never above the plot where it would cover the controls.
function Tooltip({ point, period, edge, height }: {
  point: UsagePoint
  period: Period
  edge: "left" | "center" | "right"
  height: number
}) {
  return (
    <div
      style={{ bottom: `min(calc(${height / 100} * (100% - 1.25rem) + 1.75rem), calc(100% - 3.75rem))` }}
      className={cn(
        "bg-popover text-popover-foreground ring-foreground/10 pointer-events-none absolute z-10 w-max rounded-md px-2.5 py-1.5 text-xs shadow-md ring-1",
        edge === "left" && "left-0",
        edge === "center" && "left-1/2 -translate-x-1/2",
        edge === "right" && "right-0",
      )}
    >
      <p className="text-muted-foreground">{pointLabel(point.start, period, true)}</p>
      <p className="font-medium tabular-nums">{formatBytes(total(point))}</p>
      <p className="text-muted-foreground tabular-nums">
        ↓ {formatBytes(point.tx_bytes)} · ↑ {formatBytes(point.rx_bytes)}
      </p>
    </div>
  )
}
