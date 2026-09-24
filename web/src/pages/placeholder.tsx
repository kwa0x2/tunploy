import { Card, CardContent } from "@/components/ui/card"
import { PageHeader } from "@/components/page-header"

export function PlaceholderPage({ title, description }: { title: string; description: string }) {
  return (
    <>
      <PageHeader title={title} description={description} />
      <Card>
        <CardContent className="text-muted-foreground py-10 text-center text-sm">
          Coming in a later milestone.
        </CardContent>
      </Card>
    </>
  )
}
