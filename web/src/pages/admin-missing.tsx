import { useState } from "react"
import { Loader2, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { CopyButton } from "@/components/copy-button"
import { AuthLayout } from "@/components/auth-layout"
import { useAuth } from "@/lib/auth"

const command = "docker exec -it tunploy tunploy admin create"

export function AdminMissingPage() {
  const { recheck } = useAuth()
  const [checking, setChecking] = useState(false)

  async function check() {
    setChecking(true)
    try {
      await recheck()
    } finally {
      setChecking(false)
    }
  }

  return (
    <AuthLayout className="max-w-lg">
      <Card className="shadow-xl shadow-amber-500/5">
        <CardHeader>
          <CardTitle className="text-lg">No admin account yet</CardTitle>
          <CardDescription>
            The install script normally creates it. To create it now, run this on the server:
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="bg-muted flex items-center gap-2 rounded-lg border px-3 py-2">
            <code className="min-w-0 flex-1 overflow-x-auto font-mono text-xs whitespace-nowrap">
              {command}
            </code>
            <CopyButton value={command} label="Copy command" />
          </div>
          <Button variant="outline" className="w-full" disabled={checking} onClick={() => void check()}>
            {checking ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            I've created it, continue to sign in
          </Button>
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
