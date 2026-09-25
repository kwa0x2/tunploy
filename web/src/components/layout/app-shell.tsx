import { useState } from "react"
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom"
import {
  ArchiveRestore,
  ArrowUpCircle,
  Bell,
  Globe,
  HardDrive,
  History,
  KeySquare,
  LayoutDashboard,
  LogOut,
  Menu,
  Server,
  ShieldCheck,
  SlidersHorizontal,
  Users,
} from "lucide-react"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { Logo } from "@/components/logo"
import { SiteFooter } from "@/components/site-footer"
import { ThemeToggle } from "@/components/theme-toggle"
import { useAuth } from "@/lib/auth"
import { UpdateProvider, useUpdate } from "@/lib/update"
import { cn } from "@/lib/utils"

const navigation = [
  {
    items: [
      { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
      { to: "/servers", label: "Servers", icon: Server, end: false },
      { to: "/nodes", label: "Nodes", icon: HardDrive, end: false },
      { to: "/peers", label: "Peers", icon: Users, end: false },
      { to: "/activity", label: "Activity", icon: History, end: false },
    ],
  },
  {
    label: "Settings",
    items: [
      { to: "/settings", label: "General", icon: SlidersHorizontal, end: true },
      { to: "/settings/domain", label: "Domain", icon: Globe, end: false },
      { to: "/settings/security", label: "Security", icon: ShieldCheck, end: false },
      { to: "/settings/notifications", label: "Notifications", icon: Bell, end: false },
      { to: "/settings/backups", label: "Backups", icon: ArchiveRestore, end: false },
      { to: "/settings/api-keys", label: "API keys", icon: KeySquare, end: false },
      { to: "/settings/updates", label: "Updates", icon: ArrowUpCircle, end: false },
    ],
  },
]

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex-1 space-y-6 overflow-y-auto px-3 py-4">
      {navigation.map(({ label: section, items }) => (
        <div key={section ?? "main"} className="space-y-1">
          {section && (
            <p className="text-muted-foreground px-3 pb-1 text-xs font-medium">{section}</p>
          )}
          {items.map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              onClick={onNavigate}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                )
              }
            >
              <Icon className="size-4" />
              {label}
            </NavLink>
          ))}
        </div>
      ))}
    </nav>
  )
}

function SidebarFooter({ onNavigate }: { onNavigate?: () => void }) {
  const { status } = useUpdate()
  const version = status?.current
  if (!version) return null
  return (
    <Link
      to="/settings/updates"
      onClick={onNavigate}
      className="text-muted-foreground hover:text-foreground border-t px-6 py-4 font-mono text-xs transition-colors"
    >
      Tunploy {/^\d/.test(version) ? `v${version}` : version}
    </Link>
  )
}

export function AppShell() {
  return (
    <UpdateProvider>
      <Shell />
    </UpdateProvider>
  )
}

function UpdateButton() {
  const { status, startUpdate } = useUpdate()
  const navigate = useNavigate()
  if (!status?.available || !status.latest || status.updating) return null
  return (
    <Button
      variant="outline"
      size="sm"
      className="border-emerald-500/40 text-emerald-700 hover:text-emerald-700 dark:text-emerald-400"
      onClick={() => (status.unsupported ? navigate("/settings/updates") : startUpdate())}
    >
      <ArrowUpCircle />
      <span className="hidden sm:inline">Update to</span> {status.latest.version}
    </Button>
  )
}

function Shell() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const initials = (user?.name || user?.email || "?")
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("")

  return (
    <div className="bg-muted/40 flex min-h-svh">
      <aside className="bg-background sticky top-0 hidden h-svh w-60 shrink-0 border-r md:flex md:flex-col">
        <div className="flex h-16 items-center border-b px-6">
          <Logo />
        </div>
        <NavLinks />
        <SidebarFooter />
      </aside>

      <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
        <SheetContent side="left" className="w-64 gap-0 p-0 sm:max-w-64">
          <div className="flex h-16 items-center border-b px-6">
            <SheetTitle className="sr-only">Navigation</SheetTitle>
            <Logo />
          </div>
          <NavLinks onNavigate={() => setMenuOpen(false)} />
          <SidebarFooter onNavigate={() => setMenuOpen(false)} />
        </SheetContent>
      </Sheet>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="bg-background/80 sticky top-0 z-10 flex h-16 items-center justify-between gap-2 border-b px-4 backdrop-blur md:px-6">
          <div className="flex items-center gap-2 md:hidden">
            <Button
              variant="ghost"
              size="icon"
              aria-label="Open navigation"
              onClick={() => setMenuOpen(true)}
            >
              <Menu />
            </Button>
            <Logo />
          </div>
          <div className="ml-auto flex items-center gap-1">
            <UpdateButton />
            <ThemeToggle />
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="ghost" size="icon" aria-label="Account menu" />}
              >
                <Avatar className="size-8">
                  <AvatarFallback className="text-xs">{initials}</AvatarFallback>
                </Avatar>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuGroup>
                  <DropdownMenuLabel className="font-normal">
                    <span className="block truncate font-medium">{user?.name}</span>
                    <span className="text-muted-foreground block truncate text-xs">
                      {user?.email}
                    </span>
                  </DropdownMenuLabel>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => navigate("/settings/security")}>
                  <ShieldCheck className="size-4" />
                  Security
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => void logout()}>
                  <LogOut className="size-4" />
                  Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        <main className="flex-1 px-4 py-6 md:px-6 md:py-8">
          <Outlet />
        </main>
        <SiteFooter className="px-4 pb-6 md:px-6" />
      </div>
    </div>
  )
}
