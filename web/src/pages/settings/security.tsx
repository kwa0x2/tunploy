import { useCallback, useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/form-field"
import { PageHeader } from "@/components/page-header"
import { SecurityCard } from "@/components/security-card"
import { useResource } from "@/hooks/use-resource"
import { ApiError, api } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { errorMessage } from "@/lib/format"

export function SecuritySettingsPage() {
  const httpsUrl = useResource(useCallback(() => api.httpsUrl(), []))

  return (
    <>
      <PageHeader title="Security" description="Protect the panel and your account." />
      <div className="grid max-w-2xl gap-6">
        <SecurityCard httpsUrl={httpsUrl.data ?? ""} />
        <PasswordCard />
      </div>
    </>
  )
}

const emptyPasswords = { current_password: "", new_password: "", confirmation: "" }

function PasswordCard() {
  const { user } = useAuth()
  const [values, setValues] = useState(emptyPasswords)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")
  const [busy, setBusy] = useState(false)

  const set = (key: keyof typeof emptyPasswords) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setValues((v) => ({ ...v, [key]: e.target.value }))

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFieldErrors({})
    setFormError("")
    if (values.new_password !== values.confirmation) {
      setFieldErrors({ confirmation: "Passwords do not match" })
      return
    }

    setBusy(true)
    try {
      await api.changePassword({
        current_password: values.current_password,
        new_password: values.new_password,
      })
      setValues(emptyPasswords)
      toast.success("Password changed. Other devices have been signed out.")
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
      else setFormError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card>
      <form onSubmit={handleSubmit} noValidate className="contents">
        <CardHeader>
          <CardTitle>Password</CardTitle>
          <CardDescription>
            Signed in as {user?.name} ({user?.email}).
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {formError && (
            <Alert variant="destructive">
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}
          {/* Lets password managers tie the new password to the account. */}
          <input type="email" autoComplete="username" value={user?.email ?? ""} readOnly hidden />
          <FormField id="current_password" label="Current password" error={fieldErrors.current_password}>
            <Input
              id="current_password"
              type="password"
              autoComplete="current-password"
              value={values.current_password}
              onChange={set("current_password")}
              aria-invalid={Boolean(fieldErrors.current_password)}
            />
          </FormField>
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              id="new_password"
              label="New password"
              error={fieldErrors.new_password}
              hint="At least 8 characters."
            >
              <Input
                id="new_password"
                type="password"
                autoComplete="new-password"
                value={values.new_password}
                onChange={set("new_password")}
                aria-invalid={Boolean(fieldErrors.new_password)}
              />
            </FormField>
            <FormField id="confirmation" label="Confirm new password" error={fieldErrors.confirmation}>
              <Input
                id="confirmation"
                type="password"
                autoComplete="new-password"
                value={values.confirmation}
                onChange={set("confirmation")}
                aria-invalid={Boolean(fieldErrors.confirmation)}
              />
            </FormField>
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button type="submit" disabled={busy || !values.current_password || !values.new_password}>
            {busy && <Loader2 className="animate-spin" />}
            Change password
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
