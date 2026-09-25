import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { Fingerprint, Loader2, RotateCw, ShieldCheck } from "lucide-react"
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
import { CopyButton } from "@/components/copy-button"
import { StepProgress } from "@/components/deploy-progress"
import type { StepState } from "@/components/deploy-progress"
import { FormField } from "@/components/form-field"
import { ApiError, api } from "@/lib/api"
import type { HostKeyScan, Node, NodeInput, NodeStep, PanelKey } from "@/lib/api"
import { errorMessage } from "@/lib/format"

const setupSteps: { step: NodeStep; label: string }[] = [
  { step: "connect", label: "Connected over SSH" },
  { step: "authorize", label: "Panel key added" },
  { step: "docker", label: "Docker ready" },
  { step: "wireguard", label: "Kernel supports WireGuard" },
  { step: "image", label: "WireGuard image ready" },
]

type Auth = "password" | "key" | "panel"

const auths: { value: Auth; label: string }[] = [
  { value: "password", label: "Password" },
  { value: "key", label: "Private key" },
  { value: "panel", label: "Panel key" },
]

interface Values {
  name: string
  host: string
  port: string
  username: string
  password: string
  private_key: string
  passphrase: string
}

const empty: Values = {
  name: "",
  host: "",
  port: "22",
  username: "root",
  password: "",
  private_key: "",
  passphrase: "",
}

const textareaClass =
  "border-input focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 aria-invalid:border-destructive min-h-28 w-full rounded-lg border bg-transparent px-2.5 py-2 font-mono text-xs outline-none focus-visible:ring-3"

type Stage =
  | { kind: "form" }
  | { kind: "verify"; scan: HostKeyScan }
  | { kind: "setup"; scan: HostKeyScan; state: StepState<NodeStep> }

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onAdded: (node: Node) => void
}

export function AddNodeDialog(props: Props) {
  const [busy, setBusy] = useState(false)
  return (
    <Dialog open={props.open} onOpenChange={(next) => !busy && props.onOpenChange(next)}>
      <DialogContent className="sm:max-w-lg">
        <AddNodeFlow {...props} busy={busy} setBusy={setBusy} />
      </DialogContent>
    </Dialog>
  )
}

// Mounted with the dialog's content, so every opening starts fresh.
function AddNodeFlow({ onOpenChange, onAdded, busy, setBusy }: Props & {
  busy: boolean
  setBusy: (busy: boolean) => void
}) {
  const [values, setValues] = useState<Values>(empty)
  const [auth, setAuth] = useState<Auth>("password")
  const [stage, setStage] = useState<Stage>({ kind: "form" })
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")
  const [panelKey, setPanelKey] = useState<PanelKey>()

  useEffect(() => {
    if (auth === "panel" && !panelKey) {
      api.panelKey().then(setPanelKey).catch(() => {})
    }
  }, [auth, panelKey])

  const set = (key: keyof Values) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    setValues((v) => ({ ...v, [key]: e.target.value }))

  function check(): Record<string, string> {
    const errors: Record<string, string> = {}
    if (!values.name.trim()) errors.name = "Give the node a name."
    if (!values.host.trim()) errors.host = "Enter the server's IP address or host name."
    if (!/^\d+$/.test(values.port.trim())) errors.port = "Must be a number."
    if (auth === "password" && !values.password) errors.password = "Enter the password."
    if (auth === "key" && !values.private_key.trim()) errors.private_key = "Paste the private key."
    return errors
  }

  async function scan(event: FormEvent) {
    event.preventDefault()
    setFormError("")
    const errors = check()
    setFieldErrors(errors)
    if (Object.keys(errors).length > 0) return

    setBusy(true)
    try {
      setStage({ kind: "verify", scan: await api.scanNode(values.host.trim(), Number(values.port)) })
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
      else setFormError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  async function setup(scan: HostKeyScan) {
    const input: NodeInput = {
      name: values.name.trim(),
      host: values.host.trim(),
      port: Number(values.port),
      username: values.username.trim() || "root",
      host_key: scan.host_key,
    }
    if (auth === "password") input.password = values.password
    if (auth === "key") {
      input.private_key = values.private_key
      if (values.passphrase) input.passphrase = values.passphrase
    }

    setBusy(true)
    setStage({ kind: "setup", scan, state: { status: "running", done: [] } })
    const update = (fn: (s: StepState<NodeStep>) => StepState<NodeStep>) =>
      setStage((st) => (st.kind === "setup" ? { ...st, state: fn(st.state) } : st))
    try {
      const node = await api.addNode(input, (step) => update((s) => ({ ...s, done: [...s.done, step] })))
      update((s) => ({ status: "ready", done: s.done }))
      setTimeout(() => {
        setBusy(false)
        onAdded(node)
        onOpenChange(false)
      }, 1200)
    } catch (err) {
      setBusy(false)
      if (err instanceof ApiError && err.code === "validation_failed") {
        setFieldErrors(err.fields)
        setStage({ kind: "form" })
      } else {
        update((s) => ({ status: "failed", done: s.done, error: err }))
      }
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Add node</DialogTitle>
        <DialogDescription>
          {stage.kind === "form"
            ? "Another server for VPNs. The panel connects over SSH, installs Docker if needed and keeps a connection open."
            : stage.kind === "verify"
              ? "Make sure this is the server you meant before the panel sends it any credentials."
              : `Setting up ${values.name.trim()}.`}
        </DialogDescription>
      </DialogHeader>

      {stage.kind === "form" && (
        <form onSubmit={scan} className="space-y-4" noValidate>
          {formError && (
            <Alert variant="destructive">
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}
          <FormField id="node-name" label="Name" error={fieldErrors.name}>
            <Input
              id="node-name"
              placeholder="Frankfurt"
              autoFocus
              value={values.name}
              onChange={set("name")}
              aria-invalid={Boolean(fieldErrors.name)}
            />
          </FormField>
          <div className="grid grid-cols-[1fr_6rem] gap-4">
            <FormField id="node-host" label="IP address or host" error={fieldErrors.host}>
              <Input
                id="node-host"
                placeholder="203.0.113.10"
                value={values.host}
                onChange={set("host")}
                aria-invalid={Boolean(fieldErrors.host)}
              />
            </FormField>
            <FormField id="node-port" label="SSH port" error={fieldErrors.port}>
              <Input
                id="node-port"
                inputMode="numeric"
                value={values.port}
                onChange={set("port")}
                aria-invalid={Boolean(fieldErrors.port)}
              />
            </FormField>
          </div>
          <FormField
            id="node-username"
            label="User"
            error={fieldErrors.username}
            hint="root, or a user that can run sudo without a password."
          >
            <Input
              id="node-username"
              value={values.username}
              onChange={set("username")}
              aria-invalid={Boolean(fieldErrors.username)}
            />
          </FormField>

          <FormField id="node-auth" label="Sign in with">
            <div id="node-auth" role="radiogroup" className="flex flex-wrap gap-1.5">
              {auths.map((a) => (
                <Button
                  key={a.value}
                  type="button"
                  size="sm"
                  role="radio"
                  aria-checked={auth === a.value}
                  variant={auth === a.value ? "default" : "outline"}
                  onClick={() => setAuth(a.value)}
                >
                  {a.label}
                </Button>
              ))}
            </div>
          </FormField>

          {auth === "password" && (
            <FormField
              id="node-password"
              label="Password"
              error={fieldErrors.password}
              hint="Used once to add the panel's own key, then forgotten."
            >
              <Input
                id="node-password"
                type="password"
                autoComplete="off"
                value={values.password}
                onChange={set("password")}
                aria-invalid={Boolean(fieldErrors.password)}
              />
            </FormField>
          )}
          {auth === "key" && (
            <>
              <FormField
                id="node-key"
                label="Private key"
                error={fieldErrors.private_key}
                hint="Used once to add the panel's own key, then forgotten."
              >
                <textarea
                  id="node-key"
                  spellCheck={false}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  value={values.private_key}
                  onChange={set("private_key")}
                  aria-invalid={Boolean(fieldErrors.private_key)}
                  className={textareaClass}
                />
              </FormField>
              <FormField id="node-passphrase" label="Passphrase" hint="Only if the key has one.">
                <Input
                  id="node-passphrase"
                  type="password"
                  autoComplete="off"
                  value={values.passphrase}
                  onChange={set("passphrase")}
                />
              </FormField>
            </>
          )}
          {auth === "panel" && (
            <div className="bg-muted/50 space-y-2 rounded-lg border p-3 text-sm">
              <p className="text-muted-foreground">
                Add this line to <code className="text-foreground">~/.ssh/authorized_keys</code> for the user above:
              </p>
              <div className="flex items-start gap-2">
                <code className="bg-background min-w-0 flex-1 rounded-md border p-2 font-mono text-xs break-all">
                  {panelKey?.public_key ?? "Loading…"}
                </code>
                {panelKey && <CopyButton value={panelKey.public_key} label="Copy key" />}
              </div>
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy}>
              {busy && <Loader2 className="animate-spin" />}
              Connect
            </Button>
          </DialogFooter>
        </form>
      )}

      {stage.kind === "verify" && (
        <div className="space-y-4">
          <div className="space-y-3 rounded-lg border p-4">
            <div className="flex items-center gap-2 text-sm font-medium">
              <Fingerprint className="text-primary size-4" />
              Host key of {values.host.trim()}
            </div>
            <p className="bg-muted rounded-md px-2.5 py-2 font-mono text-xs break-all">
              {stage.scan.fingerprint}
            </p>
            <p className="text-muted-foreground text-xs">
              {stage.scan.algorithm}. To compare, run this on the server:
            </p>
            <code className="bg-muted block rounded-md px-2.5 py-2 font-mono text-xs break-all">
              ssh-keygen -lf /etc/ssh/ssh_host_{keyFile(stage.scan.algorithm)}_key.pub
            </code>
            <p className="text-muted-foreground text-xs">
              The panel remembers this key and refuses to connect if it ever changes.
            </p>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setStage({ kind: "form" })}>
              Back
            </Button>
            <Button onClick={() => void setup(stage.scan)}>
              <ShieldCheck />
              Trust and set up
            </Button>
          </DialogFooter>
        </div>
      )}

      {stage.kind === "setup" && (
        <div className="space-y-4">
          <StepProgress
            steps={setupSteps}
            state={stage.state}
            ready="Node is ready."
            console="ssh"
            hint={(current) =>
              current === "docker"
                ? "If Docker is missing it gets installed now, which can take a few minutes."
                : current === "image"
                  ? "Building the WireGuard image on the node."
                  : undefined
            }
          />
          {stage.state.status === "failed" && (
            <DialogFooter>
              <Button variant="outline" onClick={() => setStage({ kind: "form" })}>
                Back
              </Button>
              <Button onClick={() => void setup(stage.scan)}>
                <RotateCw />
                Try again
              </Button>
            </DialogFooter>
          )}
        </div>
      )}
    </>
  )
}

function keyFile(algorithm: string) {
  if (algorithm.startsWith("ecdsa")) return "ecdsa"
  if (algorithm.startsWith("ssh-rsa") || algorithm.startsWith("rsa")) return "rsa"
  return "ed25519"
}

export function RenameNodeDialog({ node, onOpenChange, onSaved }: {
  node?: Node
  onOpenChange: (open: boolean) => void
  onSaved: (node: Node) => void
}) {
  const [busy, setBusy] = useState(false)
  return (
    <Dialog open={Boolean(node)} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Rename node</DialogTitle>
          <DialogDescription>The name only shows up in the panel and its emails.</DialogDescription>
        </DialogHeader>
        {node && (
          <RenameNodeForm
            key={node.id}
            node={node}
            busy={busy}
            setBusy={setBusy}
            onDone={(saved) => {
              onSaved(saved)
              onOpenChange(false)
            }}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function RenameNodeForm({ node, busy, setBusy, onDone, onCancel }: {
  node: Node
  busy: boolean
  setBusy: (busy: boolean) => void
  onDone: (node: Node) => void
  onCancel: () => void
}) {
  const [name, setName] = useState(node.name)
  const [error, setError] = useState("")

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    try {
      onDone(await api.renameNode(node.id, name))
    } catch (err) {
      setError(err instanceof ApiError ? (err.fields.name ?? err.message) : errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4" noValidate>
      <FormField id="rename-node" label="Name" error={error}>
        <Input
          id="rename-node"
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-invalid={Boolean(error)}
        />
      </FormField>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          Save
        </Button>
      </DialogFooter>
    </form>
  )
}
