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
import { Skeleton } from "@/components/ui/skeleton"
import { DnsPresets } from "@/components/dns-presets"
import { FormField } from "@/components/form-field"
import { PageHeader } from "@/components/page-header"
import { useResource } from "@/hooks/use-resource"
import { ApiError, api } from "@/lib/api"
import type { Settings } from "@/lib/api"
import { errorMessage } from "@/lib/format"

export function GeneralSettingsPage() {
  const settings = useResource(useCallback(() => api.settings(), []))

  return (
    <>
      <PageHeader title="General" description="Defaults the panel uses when you create a server." />
      <div className="grid max-w-2xl gap-6">
        {settings.data ? (
          <DefaultsCard settings={settings.data} onSaved={() => void settings.reload()} />
        ) : settings.error !== undefined ? (
          <Alert variant="destructive">
            <AlertDescription>{errorMessage(settings.error)}</AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-72 rounded-xl" />
        )}
      </div>
    </>
  )
}

const splitList = (raw: string) => raw.split(/[\s,]+/).filter(Boolean)

function DefaultsCard({ settings, onSaved }: { settings: Settings; onSaved: () => void }) {
  const [publicHost, setPublicHost] = useState(settings.public_host)
  const [dns, setDns] = useState(settings.default_dns.join(", "))
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setFieldErrors({})
    setBusy(true)
    try {
      const saved = await api.updateSettings({
        public_host: publicHost.trim(),
        default_dns: splitList(dns),
      })
      setPublicHost(saved.public_host)
      setDns(saved.default_dns.join(", "))
      toast.success("Settings saved.")
      onSaved()
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length > 0) setFieldErrors(err.fields)
      else toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const env = settings.public_host_env

  return (
    <Card>
      <form onSubmit={handleSubmit} noValidate className="contents">
        <CardHeader>
          <CardTitle>New server defaults</CardTitle>
          <CardDescription>
            Applied when you create a server. Existing servers keep their own values; change
            those in each server's settings.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <FormField
            id="public_host"
            label="Public host"
            error={fieldErrors.public_host}
            hint={
              env
                ? `The hostname or IP devices dial. Leave empty to use TUNPLOY_PUBLIC_HOST (${env}).`
                : "The hostname or IP devices dial. Leave empty to fill it from the address you open the panel with."
            }
          >
            <Input
              id="public_host"
              placeholder={env || "vpn.example.com"}
              value={publicHost}
              onChange={(e) => setPublicHost(e.target.value)}
              aria-invalid={Boolean(fieldErrors.public_host)}
            />
          </FormField>
          <FormField
            id="default_dns"
            label="DNS servers"
            error={fieldErrors.default_dns}
            hint="Comma separated. Leave empty to let devices keep their own resolver."
          >
            <Input
              id="default_dns"
              placeholder="1.1.1.1, 1.0.0.1"
              value={dns}
              onChange={(e) => setDns(e.target.value)}
              aria-invalid={Boolean(fieldErrors.default_dns)}
            />
            <DnsPresets value={dns} onPick={setDns} />
          </FormField>
        </CardContent>
        <CardFooter className="justify-end">
          <Button type="submit" disabled={busy}>
            {busy && <Loader2 className="animate-spin" />}
            Save
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
