import { useCallback } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { BackupStorageCard, BackupsCard } from "@/components/backups-card"
import { PageHeader } from "@/components/page-header"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import { errorMessage } from "@/lib/format"

export function BackupsSettingsPage() {
  const backups = useResource(useCallback(() => api.backupSettings(), []))

  return (
    <>
      <PageHeader title="Backups" description="Save the whole panel, keep copies in S3 and restore them." />
      <div className="grid max-w-6xl items-start gap-6 xl:grid-cols-2">
        {backups.data ? (
          <>
            <BackupsCard
              key={[backups.data.endpoint, backups.data.bucket, backups.data.prefix].join("|")}
              settings={backups.data}
              onChange={() => void backups.reload()}
            />
            <BackupStorageCard
              key={String(backups.data.connected)}
              settings={backups.data}
              onSaved={() => void backups.reload()}
            />
          </>
        ) : backups.error !== undefined ? (
          <Alert variant="destructive">
            <AlertDescription>{errorMessage(backups.error)}</AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-96 rounded-xl" />
        )}
      </div>
    </>
  )
}
