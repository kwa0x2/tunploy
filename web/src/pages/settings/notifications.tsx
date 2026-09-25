import { useCallback } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { NotificationsCard } from "@/components/notifications-card"
import { PageHeader } from "@/components/page-header"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import { errorMessage } from "@/lib/format"

export function NotificationsSettingsPage() {
  const notifications = useResource(useCallback(() => api.notifications(), []))

  return (
    <>
      <PageHeader title="Notifications" description="Emails the panel sends you when something happens." />
      <div className="grid max-w-2xl gap-6">
        {notifications.data ? (
          <NotificationsCard settings={notifications.data} onSaved={() => void notifications.reload()} />
        ) : notifications.error !== undefined ? (
          <Alert variant="destructive">
            <AlertDescription>{errorMessage(notifications.error)}</AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-96 rounded-xl" />
        )}
      </div>
    </>
  )
}
