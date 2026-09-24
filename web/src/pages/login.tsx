import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { CredentialsForm } from "@/components/credentials-form"
import { Logo } from "@/components/logo"
import { useAuth } from "@/lib/auth"

export function LoginPage() {
  const { login } = useAuth()

  return (
    <div className="bg-muted/40 flex min-h-svh items-center justify-center p-6">
      <div className="w-full max-w-md space-y-6">
        <Logo className="justify-center" />
        <Card>
          <CardHeader>
            <CardTitle>Sign in</CardTitle>
            <CardDescription>Welcome back. Sign in to manage your VPN servers.</CardDescription>
          </CardHeader>
          <CardContent>
            <CredentialsForm mode="login" submitLabel="Sign in" onSubmit={login} />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
