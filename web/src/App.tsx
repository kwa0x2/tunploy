import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom"
import { Loader2 } from "lucide-react"
import { Toaster } from "@/components/ui/sonner"
import { AppShell } from "@/components/layout/app-shell"
import { ActivityPage } from "@/pages/activity"
import { AdminMissingPage } from "@/pages/admin-missing"
import { LoginPage } from "@/pages/login"
import { NodesPage } from "@/pages/nodes"
import { NotFoundPage } from "@/pages/not-found"
import { OverviewPage } from "@/pages/overview"
import { PeersPage } from "@/pages/peers"
import { ServerDetailPage } from "@/pages/server-detail"
import { ServersPage } from "@/pages/servers"
import { BackupsSettingsPage } from "@/pages/settings/backups"
import { DomainSettingsPage } from "@/pages/settings/domain"
import { GeneralSettingsPage } from "@/pages/settings/general"
import { NotificationsSettingsPage } from "@/pages/settings/notifications"
import { SecuritySettingsPage } from "@/pages/settings/security"
import { UpdatesSettingsPage } from "@/pages/settings/updates"
import { AuthProvider, useAuth } from "@/lib/auth"
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
        <Route path="/settings/updates" element={<UpdatesSettingsPage />} />
      </Route>
      <Route path="/login" element={<Navigate to="/" replace />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}

export default function App() {
  return (
    <ThemeProvider>
      <BrowserRouter>
        <AuthProvider>
          <Routing />
          <Toaster />
        </AuthProvider>
      </BrowserRouter>
    </ThemeProvider>
  )
}
