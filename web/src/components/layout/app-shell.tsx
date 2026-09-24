import { useState } from "react"
import { NavLink, Outlet, useNavigate } from "react-router-dom"
import { History, LayoutDashboard, LogOut, Menu, Server, Settings, Users } from "lucide-react"
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
import { ThemeToggle } from "@/components/theme-toggle"
import { useAuth } from "@/lib/auth"
import { cn } from "@/lib/utils"

const navigation = [
  { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
  { to: "/servers", label: "Servers", icon: Server, end: false },
  { to: "/peers", label: "Peers", icon: Users, end: false },
  { to: "/activity", label: "Activity", icon: History, end: false },
  { to: "/settings", label: "Settings", icon: Settings, end: false },
]

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex-1 space-y-1 px-3 py-4">
      {navigation.map(({ to, label, icon: Icon, end }) => (
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
    </nav>
  )
}

export function AppShell() {
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
      <aside className="bg-background hidden w-60 shrink-0 border-r md:flex md:flex-col">
        <div className="flex h-16 items-center border-b px-6">
          <Logo />
        </div>
        <NavLinks />
      </aside>

      <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
        <SheetContent side="left" className="w-64 gap-0 p-0 sm:max-w-64">
          <div className="flex h-16 items-center border-b px-6">
            <SheetTitle className="sr-only">Navigation</SheetTitle>
            <Logo />
          </div>
          <NavLinks onNavigate={() => setMenuOpen(false)} />
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
                <DropdownMenuItem onClick={() => navigate("/settings")}>
                  <Settings className="size-4" />
                  Settings
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
      </div>
    </div>
  )
}
