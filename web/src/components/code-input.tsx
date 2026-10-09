import { Input } from "@/components/ui/input"

// A 2FA code: six digits, whatever else is typed or pasted.
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
