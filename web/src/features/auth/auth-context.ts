import { createContext, useContext } from "react"
import type { Credentials, User } from "@/api"

export type Status = "loading" | "setup-required" | "anonymous" | "authenticated"

export interface AuthState {
  status: Status
  user: User | null
  recheck: () => Promise<void>
  login: (creds: Credentials) => Promise<void>
  logout: () => Promise<void>
  setUser: (user: User) => void
}

export const AuthContext = createContext<AuthState | null>(null)

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider")
  return ctx
}
