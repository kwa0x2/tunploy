import { useEffect, useState } from "react"
import { LockOpen } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { AuthLayout } from "@/components/auth-layout"
import { LoginForm } from "@/components/login-form"
import { api } from "@/lib/api"
import { useAuth } from "@/lib/auth"

export function LoginPage() {
  const { login } = useAuth()
  const [httpsUrl, setHttpsUrl] = useState("")

  useEffect(() => {
    if (window.location.protocol === "https:") return
    api.httpsUrl().then(setHttpsUrl, () => {})
  }, [])

  return (
    <AuthLayout>
      {httpsUrl && (
        <Alert>
          <LockOpen />
          <AlertDescription>
            This connection is not encrypted.{" "}
            <a className="text-foreground font-medium underline" href={httpsUrl + "/login"}>
              Sign in at {httpsUrl.replace("https://", "")}
            </a>{" "}
            instead.
          </AlertDescription>
        </Alert>
      )}
      <Card className="shadow-xl shadow-amber-500/5">
        <CardHeader>
          <CardTitle className="text-lg">Sign in</CardTitle>
          <CardDescription>Welcome back. Sign in to manage your VPN servers.</CardDescription>
        </CardHeader>
        <CardContent>
          <LoginForm onSubmit={login} />
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
