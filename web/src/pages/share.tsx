import { useCallback } from "react"
import type { ReactNode } from "react"
import { useParams } from "react-router-dom"
import { Download, Loader2 } from "lucide-react"
import { QRCodeSVG } from "qrcode.react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { CopyButton } from "@/components/copy-button"
import { Logo } from "@/components/logo"
import { DeviceIcon, UsageMeter } from "@/components/status"
import { ThemeToggle } from "@/components/theme-toggle"
import { useResource } from "@/hooks/use-resource"
import { ApiError, api, sharedConfigUrl } from "@/lib/api"
import type { DeviceStatus, SharedDevice } from "@/lib/api"
import {
  errorMessage,
  formatBytes,
  formatDateTime,
  formatExpiry,
  formatSpeed,
  locationText,
  nextMonthStart,
} from "@/lib/format"
import { cn } from "@/lib/utils"

const pollMs = 30_000

// The page a device's owner gets from a share link. They have no account, so
// it stands apart from the panel and says only what they can act on.
export function SharePage() {
  const token = useParams().token ?? ""
  const device = useResource(useCallback(() => api.sharedDevice(token), [token]), pollMs)

  return (
    <div className="bg-muted/40 min-h-svh px-4 py-6 sm:px-6">
      <div className="mx-auto w-full max-w-2xl space-y-6">
        <header className="flex items-center justify-between">
          <Logo />
          <ThemeToggle />
        </header>
        {!device.data && device.error !== undefined && <LinkError error={device.error} />}
        {!device.data && device.error === undefined && (
          <div className="flex justify-center py-24">
            <Loader2 className="text-muted-foreground size-6 animate-spin" />
          </div>
        )}
        {device.data && <SharedDeviceView device={device.data} token={token} />}
      </div>
    </div>
  )
}

function LinkError({ error }: { error: unknown }) {
  const gone = error instanceof ApiError && error.status === 404
  return (
    <Card>
      <CardContent className="space-y-2 py-10 text-center">
        <h1 className="text-lg font-semibold tracking-tight">
          {gone ? "This link no longer works" : "This page could not load"}
        </h1>
        <p className="text-muted-foreground text-sm">
          {gone
            ? "It has expired or was replaced. Ask whoever sent it to you for a new one."
            : errorMessage(error)}
        </p>
      </CardContent>
    </Card>
  )
}

const statuses: Record<DeviceStatus, { label: string; tone: string }> = {
  active: { label: "Ready", tone: "bg-muted text-muted-foreground ring-foreground/10" },
  disabled: { label: "Turned off", tone: "bg-muted text-muted-foreground ring-foreground/10" },
  expired: {
    label: "Access ended",
    tone: "bg-red-500/10 text-red-700 ring-red-600/20 dark:text-red-400 dark:ring-red-400/25",
  },
  limit_reached: {
    label: "Data used up",
    tone: "bg-red-500/10 text-red-700 ring-red-600/20 dark:text-red-400 dark:ring-red-400/25",
  },
}

const connected = {
  label: "Connected",
  tone: "bg-emerald-500/10 text-emerald-700 ring-emerald-600/20 dark:text-emerald-400 dark:ring-emerald-400/25",
}

function SharedDeviceView({ device, token }: { device: SharedDevice; token: string }) {
  const status = device.status === "active" && device.online ? connected : statuses[device.status]
  const place = locationText(device)

  return (
    <>
      <Card>
        <CardContent className="space-y-6">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex min-w-0 items-center gap-3">
              <DeviceIcon className="size-10" />
              <div className="min-w-0">
                <h1 className="truncate text-lg font-semibold tracking-tight">{device.name}</h1>
                {place && <p className="text-muted-foreground text-sm">VPN in {place}</p>}
              </div>
            </div>
            <span
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ring-1 ring-inset",
                status.tone,
              )}
            >
              {status.label}
            </span>
          </div>
          <StatusNotice device={device} />
          {device.config ? (
            <ConnectSteps config={device.config} token={token} />
          ) : (
            <p className="text-muted-foreground text-sm">
              This device made its own keys, so its app already has everything it needs to connect.
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-3">
            <DataFigure device={device} />
            <Figure label="Access">
              {device.expires_at ? (
                <dd className="text-sm font-medium">
                  {device.status === "expired" ? "Ended at" : "Until"} {formatExpiry(device.expires_at)}
                </dd>
              ) : (
                <dd className="text-sm font-medium">No end date</dd>
              )}
            </Figure>
            <Figure label="Speed">
              <dd className="text-sm font-medium">
                {device.speed_limit ? `Up to ${formatSpeed(device.speed_limit)}` : "No limit"}
              </dd>
              {device.speed_limit > 0 && <dd className="text-muted-foreground text-xs">Download and upload each</dd>}
            </Figure>
          </dl>
        </CardContent>
      </Card>
      {device.link_expires_at && (
        <p className="text-muted-foreground text-center text-xs">
          This link works until {formatDateTime(device.link_expires_at)}.
        </p>
      )}
    </>
  )
}

function StatusNotice({ device }: { device: SharedDevice }) {
  let title = ""
  let body = ""
  switch (device.status) {
    case "disabled":
      title = "This device is turned off"
      body = "It cannot connect until it is turned on again."
      break
    case "expired":
      title = "Access has ended"
      body = device.expires_at ? `It ended at ${formatExpiry(device.expires_at)}.` : ""
      break
    case "limit_reached":
      title = "The data is used up"
      body =
        device.limit_period === "monthly"
          ? `It comes back on ${nextMonthStart().toLocaleDateString(undefined, { day: "numeric", month: "long" })}.`
          : "It comes back when the plan is renewed."
      break
    default:
      return null
  }
  return (
    <Alert variant="destructive">
      <AlertTitle>{title}</AlertTitle>
      {body && <AlertDescription>{body}</AlertDescription>}
    </Alert>
  )
}

const apps = [
  { label: "iPhone & iPad", href: "https://apps.apple.com/app/wireguard/id1441195209" },
  { label: "Android", href: "https://play.google.com/store/apps/details?id=com.wireguard.android" },
  { label: "Mac", href: "https://apps.apple.com/app/wireguard/id1451685025" },
  { label: "Windows", href: "https://download.wireguard.com/windows-client/wireguard-installer.exe" },
  { label: "Linux", href: "https://www.wireguard.com/install/" },
]

function ConnectSteps({ config, token }: { config: string; token: string }) {
  return (
    <div className="grid gap-6 sm:grid-cols-[auto_minmax(0,1fr)]">
      <div className="flex flex-col items-center gap-3">
        {/* A white quiet zone keeps the code scannable in dark mode too. */}
        <div className="rounded-lg bg-white p-3">
          <QRCodeSVG value={config} size={200} marginSize={0} />
        </div>
        <div className="flex flex-wrap justify-center gap-1.5">
          <Button variant="outline" nativeButton={false} render={<a href={sharedConfigUrl(token)} download />}>
            <Download />
            Download file
          </Button>
          <CopyButton value={config} label="Copy config" showLabel />
        </div>
      </div>
      <ol className="space-y-4 text-sm">
        <Step n={1} title="Install WireGuard">
          <div className="mt-2 flex flex-wrap gap-1.5">
            {apps.map((app) => (
              <Button
                key={app.label}
                variant="outline"
                size="xs"
                nativeButton={false}
                render={<a href={app.href} target="_blank" rel="noreferrer" />}
              >
                {app.label}
              </Button>
            ))}
          </div>
        </Step>
        <Step n={2} title="Add this VPN">
          <p className="text-muted-foreground">
            On a phone, tap <b className="text-foreground font-medium">+</b> in the app and scan the QR
            code shown on another screen. On a computer, or to set up the phone you are reading this
            on, download the file and import it in the app.
          </p>
        </Step>
        <Step n={3} title="Turn it on">
          <p className="text-muted-foreground">
            Switch the tunnel on. This page shows <b className="text-foreground font-medium">Connected</b>{" "}
            within a minute.
          </p>
        </Step>
      </ol>
    </div>
  )
}

function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <li className="flex gap-3">
      <span className="bg-primary text-primary-foreground grid size-6 shrink-0 place-items-center rounded-full text-xs font-semibold">
        {n}
      </span>
      <div className="min-w-0 space-y-0.5">
        <p className="font-medium">{title}</p>
        {children}
      </div>
    </li>
  )
}

// The owner's download is what the server sent them.
function DataFigure({ device }: { device: SharedDevice }) {
  const monthly = device.limit_period === "monthly"
  if (!device.data_limit) {
    const month = device.month_usage
    return (
      <Figure label="Data this month">
        <dd className="text-sm font-medium">{formatBytes(month.rx_bytes + month.tx_bytes)}</dd>
        <dd className="text-muted-foreground text-xs">No limit</dd>
      </Figure>
    )
  }
  const used = device.period_usage.rx_bytes + device.period_usage.tx_bytes
  const left = Math.max(0, device.data_limit - used)
  return (
    <Figure label={monthly ? "Data this month" : "Data"}>
      <dd className="text-sm font-medium">
        {formatBytes(used)}
        <span className="text-muted-foreground font-normal"> of {formatBytes(device.data_limit)}</span>
      </dd>
      <dd className="space-y-1">
        <UsageMeter used={used} limit={device.data_limit} className="mt-1" />
        <p className="text-muted-foreground text-xs">
          {formatBytes(left)} left
          {monthly &&
            ` · resets ${nextMonthStart().toLocaleDateString(undefined, { day: "numeric", month: "short" })}`}
        </p>
      </dd>
    </Figure>
  )
}

function Figure({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0 space-y-0.5">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      {children}
    </div>
  )
}
