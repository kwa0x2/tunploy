import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { ChevronDown, Loader2 } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/lib/api"
import type { Instance, InstanceInput, InstanceSettings } from "@/lib/api"
import { cn } from "@/lib/utils"

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: (instance: Instance) => void
} & ({ mode: "create" } | { mode: "edit"; instance: Instance })

export function InstanceFormDialog(props: Props) {
  const { open, onOpenChange } = props
  const [busy, setBusy] = useState(false)

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{props.mode === "create" ? "New VPN server" : "Server settings"}</DialogTitle>
          <DialogDescription>
            {props.mode === "create"
              ? "Pick a name and deploy. Everything else has a sensible default."
              : "Client configs carry the endpoint and port, so after changing either, every device has to re-import its config."}
          </DialogDescription>
        </DialogHeader>
        <InstanceForm {...props} busy={busy} setBusy={setBusy} />
      </DialogContent>
    </Dialog>
  )
}

interface Values {
  name: string
  endpoint: string
  listen_port: string
  address: string
  dns: string
  mtu: string
  persistent_keepalive: string
  client_allowed_ips: string
}

const empty: Values = {
  name: "",
  endpoint: "",
  listen_port: "",
  address: "",
  dns: "",
  mtu: "",
  persistent_keepalive: "",
  client_allowed_ips: "",
}

function valuesOf(s: InstanceSettings & { name?: string }): Values {
  return {
    name: s.name ?? "",
    endpoint: s.endpoint,
    listen_port: String(s.listen_port),
    address: s.address,
    dns: s.dns.join(", "),
    mtu: s.mtu ? String(s.mtu) : "",
    persistent_keepalive: String(s.persistent_keepalive),
    client_allowed_ips: s.client_allowed_ips.join(", "),
  }
}

const splitList = (raw: string) => raw.split(/[\s,]+/).filter(Boolean)

function InstanceForm(
  props: Props & { busy: boolean; setBusy: (busy: boolean) => void },
) {
  const { busy, setBusy, onSaved, onOpenChange } = props
  const isCreate = props.mode === "create"

  const [values, setValues] = useState<Values>(() =>
    props.mode === "edit" ? valuesOf(props.instance) : empty,
  )
  const [defaults, setDefaults] = useState<InstanceSettings>()
  const [advanced, setAdvanced] = useState(!isCreate)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")

  useEffect(() => {
    if (!isCreate) return
    api
      .instanceDefaults()
      .then((d) => {
        setDefaults(d)
        // Without TUNPLOY_PUBLIC_HOST the server can't know its public name,
        // but the host the admin reached the panel through usually is it.
        if (!d.endpoint) setValues((v) => ({ ...v, endpoint: window.location.hostname }))
      })
      .catch(() => {})
  }, [isCreate])

  const needsEndpoint = isCreate && defaults !== undefined && !defaults.endpoint
  const set = (key: keyof Values) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setValues((v) => ({ ...v, [key]: e.target.value }))

  function build(): { input: InstanceInput; errors: Record<string, string> } {
    const input: InstanceInput = { name: values.name }
    const errors: Record<string, string> = {}

    const int = (key: "listen_port" | "mtu" | "persistent_keepalive", fallback?: number) => {
      const raw = values[key].trim()
      if (raw === "") {
        if (fallback !== undefined) input[key] = fallback
        return
      }
      if (!/^\d+$/.test(raw)) errors[key] = "must be a whole number"
      else input[key] = Number(raw)
    }
    const text = (key: "endpoint" | "address") => {
      if (values[key].trim() !== "") input[key] = values[key].trim()
    }
    const list = (key: "dns" | "client_allowed_ips") => {
      if (values[key].trim() !== "" || !isCreate) input[key] = splitList(values[key])
    }

    text("endpoint")
    int("listen_port")
    list("dns")
    if (isCreate) {
      text("address")
    } else {
      int("mtu", 0)
      int("persistent_keepalive", 0)
      list("client_allowed_ips")
    }
    return { input, errors }
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError("")
    const { input, errors } = build()
    setFieldErrors(errors)
    if (Object.keys(errors).length > 0) return

    setBusy(true)
    try {
      const saved =
        props.mode === "create"
          ? await api.createInstance(input)
          : await api.updateInstance(props.instance.id, input)
      onSaved(saved)
      onOpenChange(false)
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        setFieldErrors(err.fields)
        const hidden = ["endpoint", "listen_port", "address", "dns"].some((f) => f in err.fields)
        if (hidden) setAdvanced(true)
      } else {
        setFormError(err instanceof ApiError ? err.message : "Something went wrong.")
      }
    } finally {
      setBusy(false)
    }
  }

  const endpointField = (
    <FormField
      id="endpoint"
      label="Public endpoint"
      error={fieldErrors.endpoint}
      hint={
        isLocal(values.endpoint)
          ? "Devices on other machines can't reach this. Use the server's public IP or domain."
          : "The hostname or IP clients dial. The port is added for you."
      }
    >
      <Input
        id="endpoint"
        placeholder={defaults?.endpoint || "vpn.example.com"}
        value={values.endpoint}
        onChange={set("endpoint")}
        aria-invalid={Boolean(fieldErrors.endpoint)}
      />
    </FormField>
  )

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      {formError && (
        <Alert variant="destructive">
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      )}

      <FormField id="name" label="Name" error={fieldErrors.name}>
        <Input
          id="name"
          placeholder="Home"
          autoFocus
          value={values.name}
          onChange={set("name")}
          aria-invalid={Boolean(fieldErrors.name)}
        />
      </FormField>

      {needsEndpoint && endpointField}

      {isCreate && (
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm"
          onClick={() => setAdvanced((a) => !a)}
        >
          <ChevronDown className={cn("size-4 transition-transform", advanced && "rotate-180")} />
          Advanced settings
        </button>
      )}

      {advanced && (
        <div className="space-y-4">
          {!needsEndpoint && endpointField}
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
          <FormField id="dns" label="DNS servers" error={fieldErrors.dns} hint="Comma separated.">
            <Input
              id="dns"
              placeholder={defaults?.dns.join(", ")}
              value={values.dns}
              onChange={set("dns")}
              aria-invalid={Boolean(fieldErrors.dns)}
            />
          </FormField>
          {!isCreate && (
            <>
              <div className="grid grid-cols-2 gap-4">
                <FormField id="mtu" label="MTU" error={fieldErrors.mtu}>
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
        </div>
      )}

      {isCreate && busy && (
        <p className="text-muted-foreground text-xs">
          The first deploy builds the WireGuard image, which takes a few seconds.
        </p>
      )}

      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          {isCreate ? (busy ? "Deploying…" : "Deploy") : "Save"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function isLocal(host: string) {
  return ["localhost", "127.0.0.1", "::1"].includes(host.trim())
}
