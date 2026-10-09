import { useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
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
import { ApiError, api } from "@/api"
import type { Node } from "@/api"
import { errorMessage } from "@/lib/format"

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
