import { PageHeader } from "@/components/page-header"
import { UpdatesCard } from "@/components/updates-card"

export function UpdatesSettingsPage() {
  return (
    <>
      <PageHeader title="Updates" description="The Tunploy version you run and newer releases." />
      <div className="grid max-w-2xl gap-6">
        <UpdatesCard />
      </div>
    </>
  )
}
