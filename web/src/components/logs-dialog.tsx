import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { RotateCw } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ApiError, api } from "@/lib/api"
import { errorMessage } from "@/lib/format"
import { cn } from "@/lib/utils"

const tail = 500
const maxLines = 2000

interface Props {
  instanceId: number
  name: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function LogsDialog({ instanceId, name, open, onOpenChange }: Props) {
  const [attempt, setAttempt] = useState(0)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{name} logs</DialogTitle>
          <DialogDescription>
            Output of the WireGuard container, newest at the bottom.
          </DialogDescription>
        </DialogHeader>
        <LogStream key={attempt} instanceId={instanceId} onReconnect={() => setAttempt((a) => a + 1)} />
      </DialogContent>
    </Dialog>
  )
}

interface Line {
  id: number
  time?: Date
  text: string
}

type StreamState = "connecting" | "live" | "ended"

function LogStream({ instanceId, onReconnect }: { instanceId: number; onReconnect: () => void }) {
  const [lines, setLines] = useState<Line[]>([])
  const [state, setState] = useState<StreamState>("connecting")
  const [error, setError] = useState<unknown>()
  const box = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)

  useEffect(() => {
    const ctrl = new AbortController()
    let nextId = 0
    let rest = ""

    const push = (raw: string[]) => {
      if (raw.length === 0) return
      const parsed = raw.map((text) => parseLine(nextId++, text))
      setLines((prev) => [...prev, ...parsed].slice(-maxLines))
    }

    api
      .instanceLogs(
        instanceId,
        { tail, follow: true },
        (chunk) => {
          setState("live")
          // A chunk can end mid-line; hold the tail back until it completes.
          const parts = (rest + chunk).split("\n")
          rest = parts.pop() ?? ""
          push(parts)
        },
        ctrl.signal,
      )
      .then(() => {
        push(rest ? [rest] : [])
        setState("ended")
      })
      .catch((err) => {
        if (ctrl.signal.aborted) return
        setError(err)
        setState("ended")
      })

    return () => ctrl.abort()
  }, [instanceId])

  // Follow only while at the bottom, so scrolling up to read isn't yanked away.
  useLayoutEffect(() => {
    const el = box.current
    if (el && pinned.current) el.scrollTop = el.scrollHeight
  }, [lines])

  const notDeployed = error instanceof ApiError && error.status === 409

  return (
    <div className="min-w-0 space-y-3">
      {error !== undefined && (
        <Alert variant={notDeployed ? "default" : "destructive"}>
          <AlertDescription>
            {notDeployed ? "This server has no container yet, so there are no logs." : errorMessage(error)}
          </AlertDescription>
        </Alert>
      )}

      <div
        ref={box}
        onScroll={(e) => {
          const el = e.currentTarget
          pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
        }}
        className="bg-muted/50 h-[60vh] overflow-auto rounded-lg border p-3 font-mono text-xs leading-relaxed"
      >
        {lines.length === 0 ? (
          <p className="text-muted-foreground">
            {state === "connecting" ? "Loading logs…" : "No output yet."}
          </p>
        ) : (
          lines.map((line) => (
            <div key={line.id} className="flex gap-3">
              {line.time && (
                <time
                  dateTime={line.time.toISOString()}
                  title={line.time.toLocaleString()}
                  className="text-muted-foreground shrink-0 select-none"
                >
                  {line.time.toLocaleTimeString([], { hour12: false })}
                </time>
              )}
              <span className="min-w-0 break-words whitespace-pre-wrap">{line.text}</span>
            </div>
          ))
        )}
      </div>

      <div className="flex items-center justify-between gap-2">
        <span className="text-muted-foreground flex items-center gap-2 text-xs">
          <span
            className={cn(
              "size-2 rounded-full",
              state === "live" ? "bg-emerald-500" : "bg-muted-foreground/40",
            )}
          />
          {state === "live" ? "Following" : state === "connecting" ? "Connecting…" : "Stream ended"}
        </span>
        {state === "ended" && !notDeployed && (
          <Button variant="outline" size="sm" onClick={onReconnect}>
            <RotateCw />
            Reconnect
          </Button>
        )}
      </div>
    </div>
  )
}

// Docker timestamps are in nanoseconds; Date takes milliseconds.
function parseLine(id: number, raw: string): Line {
  const space = raw.indexOf(" ")
  if (space > 0) {
    const time = new Date(raw.slice(0, space).replace(/(\.\d{3})\d+/, "$1"))
    if (!Number.isNaN(time.getTime())) return { id, time, text: raw.slice(space + 1) }
  }
  return { id, text: raw }
}
