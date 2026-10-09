import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom"
import { Loader2 } from "lucide-react"
import { Toaster } from "@/components/ui/sonner"
import { AppShell } from "@/app/app-shell"
import { ActivityPage } from "@/features/activity/activity-page"
import { AdminMissingPage } from "@/features/auth/admin-missing-page"
import { LoginPage } from "@/features/auth/login-page"
import { NodesPage } from "@/features/nodes/nodes-page"
import { NotFoundPage } from "@/app/not-found-page"
import { OverviewPage } from "@/features/overview/overview-page"
import { PeersPage } from "@/features/peers/peers-page"
import { ServerDetailPage } from "@/features/servers/server-detail-page"
import { ServersPage } from "@/features/servers/servers-page"
import { SharePage } from "@/features/share/share-page"
import { ApiKeysSettingsPage } from "@/features/settings/api-keys-page"
import { WebhooksSettingsPage } from "@/features/settings/webhooks-page"
import { BackupsSettingsPage } from "@/features/settings/backups-page"
import { DomainSettingsPage } from "@/features/settings/domain-page"
import { GeneralSettingsPage } from "@/features/settings/general-page"
import { NotificationsSettingsPage } from "@/features/settings/notifications-page"
import { SecuritySettingsPage } from "@/features/settings/security-page"
import { UpdatesSettingsPage } from "@/features/settings/updates-page"
import { AuthProvider, useAuth } from "@/features/auth/auth-context"
import { ThemeProvider } from "@/lib/theme"

function Routing() {
  const { status } = useAuth()

  if (status === "loading") {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Loader2 className="text-muted-foreground size-6 animate-spin" />
      </div>
    )
  }

  if (status === "setup-required") {
    return (
      <Routes>
        <Route path="*" element={<AdminMissingPage />} />
      </Routes>
    )
  }

  if (status === "anonymous") {
    return (
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
    )
  }

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<OverviewPage />} />
        <Route path="/servers" element={<ServersPage />} />
        <Route path="/servers/:id" element={<ServerDetailPage />} />
        <Route path="/nodes" element={<NodesPage />} />
        <Route path="/peers" element={<PeersPage />} />
        <Route path="/activity" element={<ActivityPage />} />
        <Route path="/settings" element={<GeneralSettingsPage />} />
        <Route path="/settings/domain" element={<DomainSettingsPage />} />
        <Route path="/settings/security" element={<SecuritySettingsPage />} />
        <Route path="/settings/notifications" element={<NotificationsSettingsPage />} />
        <Route path="/settings/backups" element={<BackupsSettingsPage />} />
        <Route path="/settings/api-keys" element={<ApiKeysSettingsPage />} />
        <Route path="/settings/webhooks" element={<WebhooksSettingsPage />} />
        <Route path="/settings/updates" element={<UpdatesSettingsPage />} />
      </Route>
      <Route path="/login" element={<Navigate to="/" replace />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}

// A share link's page is for the device's owner, who never signs in.
export default function App() {
  return (
    <ThemeProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/share/:token" element={<SharePage />} />
          <Route
            path="*"
            element={
              <AuthProvider>
                <Routing />
              </AuthProvider>
            }
          />
        </Routes>
        <Toaster />
      </BrowserRouter>
    </ThemeProvider>
  )
}
