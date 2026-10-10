import { Suspense, lazy } from "react"
import type { ComponentType } from "react"
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom"
import { Toaster } from "@/components/ui/sonner"
import { AppShell } from "@/app/app-shell"
import { Loading } from "@/app/loading"
import { NotFoundPage } from "@/app/not-found-page"
import { useAuth } from "@/features/auth/auth-context"
import { AuthProvider } from "@/features/auth/auth-provider"
import { ThemeProvider } from "@/lib/theme-provider"

// Each page is its own chunk, so a visit loads the shell and the page it opens.
function page<M>(load: () => Promise<M>, pick: (m: M) => ComponentType) {
  return lazy(() => load().then((m) => ({ default: pick(m) })))
}

const ActivityPage = page(() => import("@/features/activity/activity-page"), (m) => m.ActivityPage)
const AdminMissingPage = page(() => import("@/features/auth/admin-missing-page"), (m) => m.AdminMissingPage)
const LoginPage = page(() => import("@/features/auth/login-page"), (m) => m.LoginPage)
const NodesPage = page(() => import("@/features/nodes/nodes-page"), (m) => m.NodesPage)
const OverviewPage = page(() => import("@/features/overview/overview-page"), (m) => m.OverviewPage)
const PeersPage = page(() => import("@/features/peers/peers-page"), (m) => m.PeersPage)
const ServerDetailPage = page(() => import("@/features/servers/server-detail-page"), (m) => m.ServerDetailPage)
const ServersPage = page(() => import("@/features/servers/servers-page"), (m) => m.ServersPage)
const SharePage = page(() => import("@/features/share/share-page"), (m) => m.SharePage)
const ApiKeysSettingsPage = page(() => import("@/features/settings/api-keys-page"), (m) => m.ApiKeysSettingsPage)
const WebhooksSettingsPage = page(() => import("@/features/settings/webhooks-page"), (m) => m.WebhooksSettingsPage)
const BackupsSettingsPage = page(() => import("@/features/settings/backups-page"), (m) => m.BackupsSettingsPage)
const DomainSettingsPage = page(() => import("@/features/settings/domain-page"), (m) => m.DomainSettingsPage)
const GeneralSettingsPage = page(() => import("@/features/settings/general-page"), (m) => m.GeneralSettingsPage)
const NotificationsSettingsPage = page(() => import("@/features/settings/notifications-page"), (m) => m.NotificationsSettingsPage)
const SecuritySettingsPage = page(() => import("@/features/settings/security-page"), (m) => m.SecuritySettingsPage)
const UpdatesSettingsPage = page(() => import("@/features/settings/updates-page"), (m) => m.UpdatesSettingsPage)

function Routing() {
  const { status } = useAuth()

  if (status === "loading") return <Loading className="min-h-svh" />

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
        <Suspense fallback={<Loading className="min-h-svh" />}>
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
        </Suspense>
        <Toaster />
      </BrowserRouter>
    </ThemeProvider>
  )
}
