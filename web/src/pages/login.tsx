import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { AuthLayout } from "@/components/auth-layout"
import { LoginForm } from "@/components/login-form"
import { useAuth } from "@/lib/auth"

export function LoginPage() {
  const { login } = useAuth()

  return (
    <AuthLayout>
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
