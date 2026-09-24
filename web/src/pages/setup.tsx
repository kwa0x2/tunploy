import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { CredentialsForm } from "@/components/credentials-form"
import { Logo } from "@/components/logo"
import { useAuth } from "@/lib/auth"

export function SetupPage() {
  const { setup } = useAuth()

  return (
    <div className="bg-muted/40 flex min-h-svh items-center justify-center p-6">
      <div className="w-full max-w-md space-y-6">
        <Logo className="justify-center" />
        <Card>
          <CardHeader>
            <CardTitle>Create your admin account</CardTitle>
            <CardDescription>
              This is the first and only account with full control over your VPN infrastructure.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <CredentialsForm mode="register" submitLabel="Create account" onSubmit={setup} />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
