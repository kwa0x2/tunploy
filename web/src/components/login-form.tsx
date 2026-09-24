import { useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/form-field"
import { ApiError } from "@/lib/api"
import type { Credentials } from "@/lib/api"

export function LoginForm({ onSubmit }: { onSubmit: (credentials: Credentials) => Promise<void> }) {
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")
  const [busy, setBusy] = useState(false)

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFieldErrors({})
    setFormError("")
    setBusy(true)
    try {
      await onSubmit({ email, password })
    } catch (err) {
      if (err instanceof ApiError) {
        if (Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
        else setFormError(err.message)
      } else {
        setFormError("Something went wrong. Please try again.")
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      {formError && (
        <Alert variant="destructive">
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      )}

      <FormField id="email" label="Email" error={fieldErrors.email}>
        <Input
          id="email"
          type="email"
          autoComplete="username"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          aria-invalid={Boolean(fieldErrors.email)}
          autoFocus
          required
        />
      </FormField>

      <FormField id="password" label="Password" error={fieldErrors.password}>
        <Input
          id="password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          aria-invalid={Boolean(fieldErrors.password)}
          required
        />
      </FormField>

      <Button type="submit" className="w-full" disabled={busy}>
        {busy && <Loader2 className="animate-spin" />}
        Sign in
      </Button>
    </form>
  )
}
