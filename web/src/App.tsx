import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom"
import { Loader2 } from "lucide-react"
import { Toaster } from "@/components/ui/sonner"
import { AppShell } from "@/components/layout/app-shell"
import { AdminMissingPage } from "@/pages/admin-missing"
import { LoginPage } from "@/pages/login"
import { NotFoundPage } from "@/pages/not-found"
import { OverviewPage } from "@/pages/overview"
import { PeersPage } from "@/pages/peers"
import { ServerDetailPage } from "@/pages/server-detail"
import { ServersPage } from "@/pages/servers"
import { SettingsPage } from "@/pages/settings"
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
        <Route path="/peers" element={<PeersPage />} />
        <Route path="/settings" element={<SettingsPage />} />
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
