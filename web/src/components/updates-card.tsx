import { useState } from "react"
import { ArrowUpCircle, CircleCheck, ExternalLink, Info, Loader2, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useNow } from "@/hooks/use-now"
import { errorMessage, formatRelative } from "@/lib/format"
import { useUpdate } from "@/lib/update"

export function UpdatesCard() {
  const { status, error, check, startUpdate } = useUpdate()
  const [checking, setChecking] = useState(false)
  const now = useNow()

  async function checkNow() {
    setChecking(true)
    try {
      const st = await check()
      if (st.check_error) toast.error(st.check_error)
      else if (!st.available) toast.success("Tunploy is up to date.")
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setChecking(false)
    }
  }

  if (!status) {
    return error === undefined ? (
      <Skeleton className="h-56 rounded-xl" />
    ) : (
      <Alert variant="destructive">
        <AlertDescription>{errorMessage(error)}</AlertDescription>
      </Alert>
    )
  }
  const latest = status.latest

  return (
    <Card>
      <CardHeader>
        <CardTitle>Updates</CardTitle>
        <CardDescription>
          The panel looks for new Tunploy releases on GitHub twice a day and updates itself when
          you ask it to.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted-foreground">This panel</dt>
          <dd className="font-mono">{status.current}</dd>
          <dt className="text-muted-foreground">Latest release</dt>
          <dd>
            {latest ? (
              <span className="flex flex-wrap items-center gap-x-3">
                <span className="font-mono">{latest.version}</span>
                {latest.url && (
                  <a
                    href={latest.url}
                    target="_blank"
                    rel="noreferrer"
                    className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs underline-offset-4 hover:underline"
                  >
                    What's new
                    <ExternalLink className="size-3" />
                  </a>
                )}
              </span>
            ) : (
              <span className="text-muted-foreground">
                {status.checked_at ? "None published yet" : "Not checked yet"}
              </span>
            )}
          </dd>
          {status.checked_at && (
            <>
              <dt className="text-muted-foreground">Checked</dt>
              <dd className="text-muted-foreground">{formatRelative(status.checked_at, now)}</dd>
            </>
          )}
        </dl>

        {status.error && !status.updating && (
          <Alert variant="destructive">
            <AlertDescription className="break-words">
              The last update failed, so the panel stayed on {status.current}: {status.error}
            </AlertDescription>
          </Alert>
        )}

        {status.check_error && (
          <Alert variant="destructive">
            <AlertDescription className="break-words">
              Could not check for updates: {status.check_error}
            </AlertDescription>
          </Alert>
        )}

        {status.available && latest && !status.unsupported && (
          <div className="flex flex-wrap items-center gap-3 rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-3 text-sm">
            <ArrowUpCircle className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
            <span className="flex-1">
              Tunploy <b className="font-medium">{latest.version}</b> is available.
            </span>
            <Button size="sm" disabled={Boolean(status.updating)} onClick={startUpdate}>
              {status.updating && <Loader2 className="animate-spin" />}
              {status.updating ? "Updating…" : "Update now"}
            </Button>
          </div>
        )}

        {!status.available && latest && !status.check_error && !status.unsupported && (
          <div className="flex items-center gap-2 rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-3 text-sm">
            <CircleCheck className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
            Tunploy is up to date.
          </div>
        )}

        {status.unsupported && (
          <div className="bg-muted/50 text-muted-foreground flex gap-3 rounded-lg border p-3 text-sm">
            <Info className="mt-0.5 size-4 shrink-0" />
            <p>{status.unsupported}</p>
          </div>
        )}
      </CardContent>
      <CardFooter className="justify-end">
        <Button variant="outline" disabled={checking || Boolean(status.updating)} onClick={() => void checkNow()}>
          {checking ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          Check now
        </Button>
      </CardFooter>
    </Card>
  )
}
