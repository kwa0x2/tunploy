import { useCallback } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { DomainCard } from "@/components/domain-card"
import { PageHeader } from "@/components/page-header"
import { useResource } from "@/hooks/use-resource"
import { api } from "@/lib/api"
import { errorMessage } from "@/lib/format"

export function DomainSettingsPage() {
  const settings = useResource(useCallback(() => api.settings(), []))
  const domain = useResource(useCallback(() => api.domain(), []))
  const httpsUrl = useResource(useCallback(() => api.httpsUrl(), []))
  const { reload: reloadStatus } = domain
  const { reload: reloadUrl } = httpsUrl
  const reloadDomain = useCallback(() => {
    void reloadStatus()
    void reloadUrl()
  }, [reloadStatus, reloadUrl])

  return (
    <>
      <PageHeader title="Domain" description="The address you open this panel at, and its HTTPS certificate." />
      <div className="grid max-w-2xl gap-6">
        {domain.data ? (
          <DomainCard
            status={domain.data}
            serverAddress={settings.data?.public_host || settings.data?.public_host_env || ""}
            httpsUrl={httpsUrl.data ?? ""}
            onChange={reloadDomain}
          />
        ) : domain.error !== undefined ? (
          <Alert variant="destructive">
            <AlertDescription>{errorMessage(domain.error)}</AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-80 rounded-xl" />
        )}
      </div>
    </>
  )
}
