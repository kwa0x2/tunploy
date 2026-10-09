import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { ChevronDown, Loader2, RotateCw } from "lucide-react"
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
import { DeployProgress } from "@/components/deploy-progress"
import type { DeployState } from "@/components/deploy-progress"
import { FormField } from "@/components/form-field"
import { AdvancedFields } from "@/features/servers/advanced-fields"
import type { FormValues } from "@/features/servers/advanced-fields"
import { ApiError, api } from "@/api"
import type { Instance, InstanceInput, InstanceSettings, Node } from "@/api"
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

const empty: FormValues = {
  name: "",
  endpoint: "",
  listen_port: "",
  address: "",
  dns: "",
  dns_on_server: false,
  mtu: "",
  persistent_keepalive: "",
  client_allowed_ips: "",
  country: "",
  city: "",
}

function valuesOf(s: InstanceSettings & { name?: string }): FormValues {
  return {
    name: s.name ?? "",
    endpoint: s.endpoint,
    listen_port: String(s.listen_port),
    address: s.address,
    dns: s.dns.join(", "),
    dns_on_server: s.dns_on_server ?? false,
    mtu: s.mtu ? String(s.mtu) : "",
    persistent_keepalive: String(s.persistent_keepalive),
    client_allowed_ips: s.client_allowed_ips.join(", "),
    country: s.country,
    city: s.city ?? "",
  }
}

const splitList = (raw: string) => raw.split(/[\s,]+/).filter(Boolean)

function InstanceForm(
  props: Props & { busy: boolean; setBusy: (busy: boolean) => void },
) {
  const { busy, setBusy, onSaved, onOpenChange } = props
  const isCreate = props.mode === "create"

  const [values, setValues] = useState<FormValues>(() =>
    props.mode === "edit" ? valuesOf(props.instance) : empty,
  )
  const [defaults, setDefaults] = useState<InstanceSettings>()
  const [advanced, setAdvanced] = useState(!isCreate)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")
  const [deploy, setDeploy] = useState<DeployState | null>(null)
  const [deployPort, setDeployPort] = useState<number>()
  const [nodes, setNodes] = useState<Node[]>([])
  const [nodeId, setNodeId] = useState(0)

  useEffect(() => {
    if (!isCreate) return
    api.nodes().then(setNodes).catch(() => {})
  }, [isCreate])

  useEffect(() => {
    if (!isCreate) return
    let stale = false
    api
      .instanceDefaults(nodeId)
      .then((d) => {
        if (stale) return
        setDefaults(d)
        // The host the admin reached the panel through is usually the public one.
        setValues((v) => ({
          ...v,
          endpoint: !d.endpoint ? window.location.hostname : v.endpoint === window.location.hostname ? "" : v.endpoint,
        }))
      })
      .catch(() => {})
    return () => {
      stale = true
    }
  }, [isCreate, nodeId])

  const needsEndpoint = isCreate && defaults !== undefined && !defaults.endpoint
  const set = (key: keyof FormValues) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setValues((v) => ({ ...v, [key]: e.target.value }))

  function build(): { input: InstanceInput; errors: Record<string, string> } {
    const input: InstanceInput = { name: values.name }
    if (isCreate && nodeId) input.node_id = nodeId
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
    const text = (key: "endpoint" | "address" | "country" | "city") => {
      if (values[key].trim() !== "") input[key] = values[key].trim()
    }
    const list = (key: "dns" | "client_allowed_ips") => {
      if (values[key].trim() !== "" || !isCreate) input[key] = splitList(values[key])
    }

    text("endpoint")
    int("listen_port")
    list("dns")
    if (values.dns_on_server || !isCreate) input.dns_on_server = values.dns_on_server
    if (isCreate) {
      text("address")
      // Left out, the panel guesses the country from the endpoint.
      text("country")
      text("city")
    } else {
      input.country = values.country.trim()
      input.city = values.city.trim()
      int("mtu", 0)
      int("persistent_keepalive", 0)
      list("client_allowed_ips")
    }
    return { input, errors }
  }

  function showFieldErrors(fields: Record<string, string>) {
    setFieldErrors(fields)
    const hidden = ["endpoint", "listen_port", "address", "dns", "country", "city"].some((f) => f in fields)
    if (hidden) setAdvanced(true)
  }

  async function provision(input: InstanceInput) {
    setBusy(true)
    setDeployPort(input.listen_port ?? defaults?.listen_port)
    setDeploy({ status: "running", done: [] })
    try {
      const saved = await api.provisionInstance(input, (step) =>
        setDeploy((d) => d && { ...d, done: [...d.done, step] }),
      )
      setDeploy((d) => ({ status: "ready", done: d?.done ?? [] }))
      setTimeout(() => {
        setBusy(false)
        onSaved(saved)
        onOpenChange(false)
      }, 1200)
    } catch (err) {
      setBusy(false)
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        setDeploy(null)
        showFieldErrors(err.fields)
      } else {
        setDeploy((d) => ({ status: "failed", done: d?.done ?? [], error: err }))
      }
    }
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFormError("")
    const { input, errors } = build()
    setFieldErrors(errors)
    if (Object.keys(errors).length > 0) return

    if (props.mode === "create") {
      await provision(input)
      return
    }

    setBusy(true)
    try {
      onSaved(await api.updateInstance(props.instance.id, input))
      onOpenChange(false)
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) {
        showFieldErrors(err.fields)
      } else {
        setFormError(err instanceof ApiError ? err.message : "Something went wrong.")
      }
    } finally {
      setBusy(false)
    }
  }

  if (deploy) {
    return (
      <div className="space-y-4">
        <DeployProgress state={deploy} port={deployPort} />
        {deploy.status === "failed" && (
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeploy(null)}>
              Back
            </Button>
            <Button onClick={() => void provision(build().input)}>
              <RotateCw />
              Try again
            </Button>
          </DialogFooter>
        )}
      </div>
    )
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

      {nodes.length > 1 && (
        <FormField
          id="node"
          label="Node"
          error={fieldErrors.node_id}
          hint="The machine the server runs on. It can't move later."
        >
          <select
            id="node"
            value={nodeId}
            onChange={(e) => setNodeId(Number(e.target.value))}
            className="border-input focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 h-8 w-full rounded-lg border bg-transparent px-2 text-base outline-none focus-visible:ring-3 md:text-sm"
          >
            {nodes.map((n) => (
              <option key={n.id} value={n.id} disabled={!n.local && n.status.state !== "online"}>
                {n.name}
                {n.local ? "" : ` (${n.host})`}
                {!n.local && n.status.state !== "online" ? ` · ${n.status.state}` : ""}
              </option>
            ))}
          </select>
        </FormField>
      )}

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
          <AdvancedFields
            values={values}
            onChange={(update) => setValues((v) => ({ ...v, ...update }))}
            errors={fieldErrors}
            defaults={defaults}
            isCreate={isCreate}
            savedDnsOnServer={props.mode === "edit" && props.instance.dns_on_server}
          />
        </div>
      )}

      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          {isCreate ? "Deploy" : "Save"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function isLocal(host: string) {
  return ["localhost", "127.0.0.1", "::1"].includes(host.trim())
}
