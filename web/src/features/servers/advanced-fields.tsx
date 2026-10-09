import type { ChangeEvent } from "react"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { DnsPresets } from "@/components/dns-presets"
import { FormField } from "@/components/form-field"
import type { InstanceSettings } from "@/api"

export interface FormValues {
  name: string
  endpoint: string
  listen_port: string
  address: string
  dns: string
  dns_on_server: boolean
  mtu: string
  persistent_keepalive: string
  client_allowed_ips: string
  country: string
  city: string
}

// The settings past name and endpoint; the form owns their state.
export function AdvancedFields({
  values,
  onChange,
  errors: fieldErrors,
  defaults,
  isCreate,
  savedDnsOnServer,
}: {
  values: FormValues
  onChange: (update: Partial<FormValues>) => void
  errors: Record<string, string>
  defaults?: InstanceSettings
  isCreate: boolean
  savedDnsOnServer?: boolean
}) {
  const set = (key: keyof FormValues) => (e: ChangeEvent<HTMLInputElement>) => onChange({ [key]: e.target.value })

  return (
    <>
      <div className="grid grid-cols-2 gap-4">
        <FormField id="listen_port" label="UDP port" error={fieldErrors.listen_port}>
          <Input
            id="listen_port"
            inputMode="numeric"
            placeholder={defaults ? String(defaults.listen_port) : ""}
            value={values.listen_port}
            onChange={set("listen_port")}
            aria-invalid={Boolean(fieldErrors.listen_port)}
          />
        </FormField>
        <FormField
          id="address"
          label="Server address"
          error={fieldErrors.address}
          hint={isCreate ? undefined : "Fixed once created."}
        >
          <Input
            id="address"
            placeholder={defaults?.address}
            value={values.address}
            onChange={set("address")}
            disabled={!isCreate}
            aria-invalid={Boolean(fieldErrors.address)}
          />
        </FormField>
      </div>
      <div className="grid grid-cols-[6rem_1fr] gap-4">
        <FormField id="country" label="Country" error={fieldErrors.country}>
          <Input
            id="country"
            maxLength={2}
            placeholder={defaults?.country || "DE"}
            value={values.country}
            onChange={(e) => onChange({ country: e.target.value.toUpperCase() })}
            aria-invalid={Boolean(fieldErrors.country)}
          />
        </FormField>
        <FormField id="city" label="City" error={fieldErrors.city}>
          <Input
            id="city"
            placeholder="Frankfurt"
            value={values.city}
            onChange={set("city")}
            aria-invalid={Boolean(fieldErrors.city)}
          />
        </FormField>
      </div>
      <FormField id="dns" label="DNS servers" error={fieldErrors.dns} hint="Comma separated.">
        <Input
          id="dns"
          placeholder={defaults?.dns.join(", ")}
          value={values.dns}
          onChange={set("dns")}
          aria-invalid={Boolean(fieldErrors.dns)}
        />
        <DnsPresets value={values.dns} onPick={(dns) => onChange({ dns })} />
      </FormField>
      <div className="flex items-start justify-between gap-4">
        <label htmlFor="dns_on_server" className="text-sm">
          <span className="font-medium">Resolve on the server</span>
          <span className="text-muted-foreground block text-xs">
            Devices ask this server, which caches answers and forwards to the DNS servers
            above. Change those later without new configs.
            {!isCreate &&
              values.dns_on_server !== (savedDnsOnServer) &&
              " Devices need their config again after this change."}
          </span>
        </label>
        <Switch
          id="dns_on_server"
          checked={values.dns_on_server}
          onCheckedChange={(on) => onChange({ dns_on_server: on })}
        />
      </div>
      {!isCreate && (
        <>
          <div className="grid grid-cols-2 gap-4">
            <FormField
              id="mtu"
              label="MTU"
              error={fieldErrors.mtu}
              hint="Try 1380 or 1280 if some sites hang on mobile or PPPoE networks."
            >
              <Input
                id="mtu"
                inputMode="numeric"
                placeholder="1420"
                value={values.mtu}
                onChange={set("mtu")}
                aria-invalid={Boolean(fieldErrors.mtu)}
              />
            </FormField>
            <FormField
              id="persistent_keepalive"
              label="Keepalive (s)"
              error={fieldErrors.persistent_keepalive}
            >
              <Input
                id="persistent_keepalive"
                inputMode="numeric"
                placeholder="0 to disable"
                value={values.persistent_keepalive}
                onChange={set("persistent_keepalive")}
                aria-invalid={Boolean(fieldErrors.persistent_keepalive)}
              />
            </FormField>
          </div>
          <FormField
            id="client_allowed_ips"
            label="Routed through the tunnel"
            error={fieldErrors.client_allowed_ips}
            hint="0.0.0.0/0, ::/0 sends all traffic. List subnets for split tunneling. Takes effect when clients re-import their config."
          >
            <Input
              id="client_allowed_ips"
              value={values.client_allowed_ips}
              onChange={set("client_allowed_ips")}
              aria-invalid={Boolean(fieldErrors.client_allowed_ips)}
            />
          </FormField>
        </>
          )}
    </>
  )
}
