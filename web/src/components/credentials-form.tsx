import { useState } from "react"
import type { FormEvent } from "react"
import { Loader2 } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ApiError } from "@/lib/api"
import type { Credentials, Registration } from "@/lib/api"

type Props = { submitLabel: string } & (
  | { mode: "login"; onSubmit: (credentials: Credentials) => Promise<void> }
  | { mode: "register"; onSubmit: (registration: Registration) => Promise<void> }
)

export function CredentialsForm(props: Props) {
  const { submitLabel, mode } = props
  const isRegister = mode === "register"

  const [name, setName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")
  const [busy, setBusy] = useState(false)

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFieldErrors({})
    setFormError("")

    if (isRegister && password !== confirmation) {
      setFieldErrors({ confirmation: "Passwords do not match" })
      return
    }

    setBusy(true)
    try {
      if (props.mode === "register") {
        await props.onSubmit({ name, email, password })
      } else {
        await props.onSubmit({ email, password })
      }
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

      {isRegister && (
        <div className="space-y-2">
          <Label htmlFor="name">Name</Label>
          <Input
            id="name"
            autoComplete="name"
            placeholder="Ada Lovelace"
            value={name}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={Boolean(fieldErrors.name)}
            required
          />
          {fieldErrors.name && <p className="text-destructive text-sm">{fieldErrors.name}</p>}
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="email">Email</Label>
        <Input
          id="email"
          type="email"
          autoComplete="username"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          aria-invalid={Boolean(fieldErrors.email)}
          required
        />
        {fieldErrors.email && <p className="text-destructive text-sm">{fieldErrors.email}</p>}
      </div>

      <div className="space-y-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          type="password"
          autoComplete={isRegister ? "new-password" : "current-password"}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          aria-invalid={Boolean(fieldErrors.password)}
          required
        />
        {fieldErrors.password && <p className="text-destructive text-sm">{fieldErrors.password}</p>}
      </div>

      {isRegister && (
        <div className="space-y-2">
          <Label htmlFor="confirmation">Confirm password</Label>
          <Input
            id="confirmation"
            type="password"
            autoComplete="new-password"
            value={confirmation}
            onChange={(e) => setConfirmation(e.target.value)}
            aria-invalid={Boolean(fieldErrors.confirmation)}
            required
          />
          {fieldErrors.confirmation && (
            <p className="text-destructive text-sm">{fieldErrors.confirmation}</p>
          )}
        </div>
      )}

      <Button type="submit" className="w-full" disabled={busy}>
        {busy && <Loader2 className="size-4 animate-spin" />}
        {submitLabel}
      </Button>
    </form>
  )
}
