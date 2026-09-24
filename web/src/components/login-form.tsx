import { useState } from "react"
import type { FormEvent } from "react"
import { ArrowLeft, Loader2, ShieldCheck } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/form-field"
import { ApiError } from "@/lib/api"
import type { Credentials } from "@/lib/api"

export function LoginForm({ onSubmit }: { onSubmit: (credentials: Credentials) => Promise<void> }) {
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [needsCode, setNeedsCode] = useState(false)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState("")
  const [busy, setBusy] = useState(false)

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFieldErrors({})
    setFormError("")
    setBusy(true)
    try {
      await onSubmit(needsCode ? { email, password, code } : { email, password })
    } catch (err) {
      if (err instanceof ApiError && err.code === "totp_required") {
        setNeedsCode(true)
      } else if (err instanceof ApiError && err.code === "totp_invalid") {
        setCode("")
        setFieldErrors({ code: err.message })
      } else if (err instanceof ApiError) {
        if (Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
        else setFormError(err.message)
      } else {
        setFormError("Something went wrong. Please try again.")
      }
    } finally {
      setBusy(false)
    }
  }

  function back() {
    setNeedsCode(false)
    setCode("")
    setPassword("")
    setFieldErrors({})
    setFormError("")
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      {formError && (
        <Alert variant="destructive">
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      )}

      {needsCode ? (
        <>
          <div className="flex items-start gap-3 rounded-lg border bg-amber-500/5 p-3 text-sm">
            <ShieldCheck className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
            <p className="text-muted-foreground">
              Open your authenticator app and enter the 6-digit code for{" "}
              <span className="text-foreground font-medium">{email}</span>.
            </p>
          </div>
          <FormField id="code" label="Authentication code" error={fieldErrors.code}>
            <CodeInput id="code" value={code} onChange={setCode} invalid={Boolean(fieldErrors.code)} autoFocus />
          </FormField>
          <Button type="submit" className="w-full" disabled={busy || code.length < 6}>
            {busy && <Loader2 className="animate-spin" />}
            Verify
          </Button>
          <Button type="button" variant="ghost" className="w-full" onClick={back} disabled={busy}>
            <ArrowLeft />
            Back
          </Button>
        </>
      ) : (
        <>
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
        </>
      )}
    </form>
  )
}

export function CodeInput({
  id,
  value,
  onChange,
  invalid,
  autoFocus,
}: {
  id: string
  value: string
  onChange: (value: string) => void
  invalid?: boolean
  autoFocus?: boolean
}) {
  return (
    <Input
      id={id}
      inputMode="numeric"
      autoComplete="one-time-code"
      placeholder="123456"
      maxLength={6}
      className="font-mono tracking-[0.3em]"
      value={value}
      onChange={(e) => onChange(e.target.value.replace(/\D/g, "").slice(0, 6))}
      aria-invalid={invalid}
      autoFocus={autoFocus}
    />
  )
}
