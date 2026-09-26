import { Button } from "@/components/ui/button"

const presets = [
  { name: "Cloudflare", hint: "Fast, no filtering", dns: ["1.1.1.1", "1.0.0.1"] },
  { name: "Quad9", hint: "Blocks malware sites", dns: ["9.9.9.9", "149.112.112.112"] },
  { name: "AdGuard", hint: "Blocks ads and trackers", dns: ["94.140.14.14", "94.140.15.15"] },
  { name: "Cloudflare Family", hint: "Blocks malware and adult sites", dns: ["1.1.1.3", "1.0.0.3"] },
  { name: "Google", hint: "No filtering", dns: ["8.8.8.8", "8.8.4.4"] },
]

// Fills a comma-separated DNS field with a well-known provider.
export function DnsPresets({ value, onPick }: { value: string; onPick: (dns: string) => void }) {
  const current = value.split(/[\s,]+/).filter(Boolean).join(", ")
  return (
    <div className="flex flex-wrap gap-1.5">
      {presets.map((p) => {
        const dns = p.dns.join(", ")
        return (
          <Button
            key={p.name}
            type="button"
            size="xs"
            variant={current === dns ? "secondary" : "outline"}
            title={`${p.hint}: ${dns}`}
            aria-pressed={current === dns}
            onClick={() => onPick(dns)}
          >
            {p.name}
          </Button>
        )
      })}
    </div>
  )
}
