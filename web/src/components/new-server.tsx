import { useNavigate } from "react-router-dom"
import { Plus, Server } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { InstanceFormDialog } from "@/components/instance-form-dialog"

export function NewServerDialog({ open, onOpenChange }: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  return (
    <InstanceFormDialog
      mode="create"
      open={open}
      onOpenChange={onOpenChange}
      onSaved={(instance) => {
        toast.success(`${instance.name} is up and listening on UDP ${instance.listen_port}.`)
        navigate(`/servers/${instance.id}`)
      }}
    />
  )
}

export function EmptyServers({ onCreate }: { onCreate: () => void }) {
  return (
    <Card>
      <CardContent className="flex flex-col items-center gap-4 py-12 text-center">
        <div className="grid size-14 place-items-center rounded-2xl bg-linear-to-br from-yellow-300 to-amber-500 text-yellow-950 shadow-lg shadow-amber-500/25">
          <Server className="size-6" />
        </div>
        <div className="space-y-1">
          <p className="font-medium">No VPN server yet</p>
          <p className="text-muted-foreground max-w-sm text-sm">
            Deploy a WireGuard server in one click. Subnet, port and keys are picked for you.
          </p>
        </div>
        <Button onClick={onCreate}>
          <Plus />
          Create your first server
        </Button>
      </CardContent>
    </Card>
  )
}
