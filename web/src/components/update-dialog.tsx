import { useEffect, useRef, useState } from "react"
import type { ReactNode } from "react"
import { CircleCheck, Loader2 } from "lucide-react"
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
import { ApiError, api } from "@/lib/api"
import { errorMessage } from "@/lib/format"

type Phase = "confirm" | "downloading" | "restarting" | "done" | "failed"

const pollMs = 2000
// Past this the updater has given up or hangs; either way someone should look.
const slowAfterMs = 6 * 60 * 1000

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  current: string
  target: string
  onFailed: () => void
}

export function UpdateDialog({ open, onOpenChange, current, target, onFailed }: Props) {
  const [phase, setPhase] = useState<Phase>("confirm")
  const [error, setError] = useState("")
  const [slow, setSlow] = useState(false)
  const [starting, setStarting] = useState(false)
  const startedAt = useRef(0)
  const running = phase === "downloading" || phase === "restarting"

  useEffect(() => {
    if (!running) return
    const timer = setInterval(async () => {
      if (Date.now() - startedAt.current > slowAfterMs) setSlow(true)
      try {
        const st = await api.updateStatus()
        if (st.current === target) {
          setPhase("done")
          setTimeout(() => window.location.reload(), 1500)
        } else if (st.updating) {
          setPhase((p) => (p === "restarting" ? p : "downloading"))
        } else {
          // Back on the old version: the updater rolled back, or never got going.
          setError(st.error || "The panel came back on the old version.")
          setPhase("failed")
          onFailed()
        }
      } catch (err) {
        // The new panel keeps the sessions, but a reload sorts out any surprise.
        if (err instanceof ApiError && err.status === 401) window.location.reload()
        else setPhase("restarting")
      }
    }, pollMs)
    return () => clearInterval(timer)
  }, [running, target, onFailed])

  async function start() {
    setStarting(true)
    setError("")
    try {
      await api.startUpdate()
      startedAt.current = Date.now()
      setSlow(false)
      setPhase("downloading")
    } catch (err) {
      setError(errorMessage(err))
      setPhase("failed")
    } finally {
      setStarting(false)
    }
  }

  function handleOpenChange(next: boolean) {
    if (running || phase === "done" || starting) return
    onOpenChange(next)
    if (!next) {
      setPhase("confirm")
      setError("")
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent showCloseButton={!running && phase !== "done"}>
        <DialogHeader>
          <DialogTitle>Update to Tunploy {target}</DialogTitle>
          {phase === "confirm" && (
            <DialogDescription>
              The panel downloads {target} and restarts on it, which takes about a minute. VPN
              servers keep running and connected devices stay connected. If {target} doesn't
              start, {current} comes back on its own.
            </DialogDescription>
          )}
        </DialogHeader>

        {running && (
          <div className="space-y-3">
            <Step done={phase === "restarting"} active={phase === "downloading"}>
              Downloading {target}
            </Step>
            <Step done={false} active={phase === "restarting"}>
              Restarting the panel
            </Step>
            {slow && (
              <p className="text-muted-foreground text-xs">
                This is taking longer than it should. On the server,{" "}
                <code className="font-mono">docker logs tunploy</code> shows what the panel is doing.
              </p>
            )}
          </div>
        )}

        {phase === "done" && (
          <div className="flex items-center gap-2 text-sm">
            <CircleCheck className="size-4 text-emerald-600 dark:text-emerald-400" />
            Updated to {target}. Reloading…
          </div>
        )}

        {phase === "failed" && (
          <Alert variant="destructive">
            <AlertDescription className="break-words">
              The update failed, and the panel is still on {current}: {error}
            </AlertDescription>
          </Alert>
        )}

        {(phase === "confirm" || phase === "failed") && (
          <DialogFooter>
            <Button variant="outline" disabled={starting} onClick={() => handleOpenChange(false)}>
              {phase === "failed" ? "Close" : "Cancel"}
            </Button>
            <Button disabled={starting} onClick={() => void start()}>
              {starting && <Loader2 className="animate-spin" />}
              {phase === "failed" ? "Try again" : "Update now"}
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  )
}

function Step({ done, active, children }: { done: boolean; active: boolean; children: ReactNode }) {
  return (
    <div className={active || done ? "flex items-center gap-2 text-sm" : "text-muted-foreground flex items-center gap-2 text-sm"}>
      {done ? (
        <CircleCheck className="size-4 text-emerald-600 dark:text-emerald-400" />
      ) : active ? (
        <Loader2 className="size-4 animate-spin" />
      ) : (
        <span className="size-4 rounded-full border" />
      )}
      {children}
    </div>
  )
}
