import { AlertCircle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import type { Resource } from "@/lib/api"

export function ResourceState<T>({
  value,
  children,
}: {
  value: Resource<T>
  children: (data: T) => React.ReactNode
}) {
  if (value.loading) {
    return <div aria-busy="true" aria-label="Loading data" className="space-y-3">
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-3/4" />
    </div>
  }
  if (value.error) {
    return <Alert variant="destructive">
      <AlertCircle aria-hidden="true" />
      <AlertTitle>Could not retrieve this resource</AlertTitle>
      <AlertDescription>{value.error}</AlertDescription>
    </Alert>
  }
  if (value.data === null) return <p className="text-sm text-muted-foreground">No response was returned.</p>
  return <>{children(value.data)}</>
}

export function EmptyState({ title, description }: { title: string; description?: string }) {
  return <div className="rounded-lg border border-dashed px-5 py-10 text-center">
    <p className="font-medium">{title}</p>
    {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
  </div>
}
