import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"
import { ApiError, api } from "@/lib/api"
import type { Credentials, User } from "@/lib/api"

type Status = "loading" | "setup-required" | "anonymous" | "authenticated"

interface AuthState {
  status: Status
  user: User | null
  recheck: () => Promise<void>
  login: (creds: Credentials) => Promise<void>
  logout: () => Promise<void>
  setUser: (user: User) => void
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>("loading")
  const [user, setUser] = useState<User | null>(null)

  const bootstrap = useCallback(async () => {
    try {
      const { setup_required } = await api.setupStatus()
      if (setup_required) {
        setUser(null)
        setStatus("setup-required")
        return
      }
      const me = await api.me()
      setUser(me)
      setStatus("authenticated")
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setUser(null)
        setStatus("anonymous")
        return
      }
      throw err
    }
  }, [])

  useEffect(() => {
    bootstrap().catch(() => setStatus("anonymous"))
  }, [bootstrap])

  const value = useMemo<AuthState>(
    () => ({
      status,
      user,
      recheck: bootstrap,
      login: async (creds) => {
        setUser(await api.login(creds))
        setStatus("authenticated")
      },
      logout: async () => {
        await api.logout()
        setUser(null)
        setStatus("anonymous")
      },
      setUser,
    }),
    [status, user, bootstrap],
  )

  return <AuthContext value={value}>{children}</AuthContext>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider")
  return ctx
}
