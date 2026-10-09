import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/form-field"
import { presetDate } from "@/features/peers/limits"
import type { Limits, LimitErrors } from "@/features/peers/limits"
import type { LimitPeriod } from "@/api"

const presets = [
  { label: "Today", date: () => presetDate(0) },
  { label: "1 week", date: () => presetDate(7) },
  { label: "1 month", date: () => presetDate(0, 1) },
]

const periods: { value: LimitPeriod; label: string }[] = [
  { value: "monthly", label: "Every month" },
  { value: "total", label: "In total" },
]

export function LimitFields({ value, onChange, errors }: {
  value: Limits
  onChange: (value: Limits) => void
  errors: LimitErrors
}) {
  const error = errors.size
  return (
    <>
      <FormField
        id="peer-limit"
        label="Data limit"
        error={error}
        hint={
          value.period === "monthly"
            ? "Download and upload together. Starts again on the 1st. Leave empty for no limit."
            : "Download and upload together. Counts until you reset the usage. Leave empty for no limit."
        }
      >
        <div className="flex gap-1.5">
          <Input
            id="peer-limit"
            type="number"
            min={0}
            step="any"
            inputMode="decimal"
            placeholder="No limit"
            value={value.size}
            onChange={(e) => onChange({ ...value, size: e.target.value })}
            aria-invalid={Boolean(error)}
          />
          <div role="radiogroup" aria-label="Unit" className="flex shrink-0 gap-1">
            {(["MB", "GB"] as const).map((unit) => (
              <Button
                key={unit}
                type="button"
                role="radio"
                aria-checked={value.unit === unit}
                variant={value.unit === unit ? "default" : "outline"}
                onClick={() => onChange({ ...value, unit })}
              >
                {unit}
              </Button>
            ))}
          </div>
        </div>
        <div role="radiogroup" aria-label="Counted" className="flex flex-wrap gap-1.5">
          {periods.map((p) => (
            <Button
              key={p.value}
              type="button"
              size="xs"
              role="radio"
              aria-checked={value.period === p.value}
              variant={value.period === p.value ? "default" : "outline"}
              onClick={() => onChange({ ...value, period: p.value })}
            >
              {p.label}
            </Button>
          ))}
        </div>
      </FormField>
      <FormField
        id="peer-speed"
        label="Speed limit"
        error={errors.speed}
        hint="Download and upload each. The device stays connected, only slower. Leave empty for no limit."
      >
        <div className="flex items-center gap-1.5">
          <Input
            id="peer-speed"
            type="number"
            min={0}
            step="any"
            inputMode="decimal"
            placeholder="No limit"
            value={value.speed}
            onChange={(e) => onChange({ ...value, speed: e.target.value })}
            aria-invalid={Boolean(errors.speed)}
          />
          <span className="text-muted-foreground shrink-0 text-sm">Mbit/s</span>
        </div>
      </FormField>
      <FormField
        id="peer-until"
        label="Access until"
        hint={value.until ? "The device is cut off when this day ends." : "No end date."}
      >
        <Input
          id="peer-until"
          type="date"
          min={presetDate(0)}
          value={value.until}
          onChange={(e) => onChange({ ...value, until: e.target.value })}
        />
        <div className="flex flex-wrap gap-1.5">
          {presets.map((p) => {
            const date = p.date()
            return (
              <Button
                key={p.label}
                type="button"
                size="xs"
                variant={value.until === date ? "secondary" : "outline"}
                onClick={() => onChange({ ...value, until: date })}
              >
                {p.label}
              </Button>
            )
          })}
          <Button
            type="button"
            size="xs"
            variant={value.until ? "outline" : "secondary"}
            onClick={() => onChange({ ...value, until: "" })}
          >
            Never
          </Button>
        </div>
      </FormField>
    </>
  )
}
