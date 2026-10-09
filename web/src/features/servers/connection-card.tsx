import type { ReactNode } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { CopyButton } from "@/components/copy-button"
import type { Instance } from "@/api"
import { endpointOf } from "@/lib/format"

export function ConnectionCard({ instance }: { instance: Instance }) {
  const rows: [string, ReactNode, string?][] = [
    ["Endpoint", endpointOf(instance), endpointOf(instance)],
    ["Server address", instance.address],
    ["Public key", instance.public_key, instance.public_key],
    [
      "DNS",
      instance.dns_on_server
        ? `${instance.address.split("/")[0]} on this server, forwarding to ${instance.dns.join(", ")}`
        : instance.dns.length
          ? instance.dns.join(", ")
          : "None",
    ],
    ["MTU", instance.mtu || "1420"],
    ["Keepalive", instance.persistent_keepalive ? `${instance.persistent_keepalive}s` : "Off"],
    ["Routed", instance.client_allowed_ips.join(", ")],
  ]

  return (
    <Card className="self-start">
      <CardHeader>
        <CardTitle>Connection</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="space-y-3 text-sm">
          {rows.map(([label, value, copy]) => (
            <div key={label} className="space-y-0.5">
              <dt className="text-muted-foreground text-xs">{label}</dt>
              <dd className="flex items-center gap-1 font-mono text-xs break-all">
                <span className="min-w-0">{value}</span>
                {copy && <CopyButton value={copy} label={`Copy ${label.toLowerCase()}`} />}
              </dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}
