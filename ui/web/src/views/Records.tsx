import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table"
import { EmptyState, ResourceState } from "@/components/ResourceState"
import { list, text, statusVariant, useApi, asRecord } from "@/lib/api"

export type ResourceDefinition = {
  title: string
  description: string
  path: string
  collectionKeys?: string[]
}

function DisplayValue({ label, value }: { label: string; value: unknown }) {
  if (typeof value === "string" || typeof value === "number") {
    return <div className="rounded-md border px-3 py-2">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 break-words text-sm">{String(value)}</dd>
    </div>
  }
  return null
}

export function Records({ definition, revision }: { definition: ResourceDefinition; revision: number }) {
  const response = useApi<unknown>(definition.path, revision)
  return <Card>
    <CardHeader><CardTitle>{definition.title}</CardTitle><CardDescription>{definition.description}</CardDescription></CardHeader>
    <CardContent>
      <ResourceState value={response}>{value => {
        const records = list(value, definition.collectionKeys ?? ["items", "results", "tasks", "records", "events", "recipes", "requests", "sessions", "profiles", "plans", "nodes", "goals", "tools"])
        if (records.length > 0) {
          return <Table>
            <TableHeader><TableRow><TableHead>Record</TableHead><TableHead>Details</TableHead><TableHead>Status</TableHead></TableRow></TableHeader>
            <TableBody>{records.slice(0, 70).map((item, i) =>
              <TableRow key={text(item, "id", "name", "key") || i}>
                <TableCell className="max-w-xs truncate font-medium">{text(item, "title", "name", "label", "id", "action", "key") || "Untitled"}</TableCell>
                <TableCell className="max-w-sm truncate text-muted-foreground">{text(item, "description", "summary", "notes", "kind", "role", "at", "due")}</TableCell>
                <TableCell>{text(item, "status", "state", "mode") && <Badge variant={statusVariant(text(item, "status", "state", "mode"))}>{text(item, "status", "state", "mode")}</Badge>}</TableCell>
              </TableRow>)}</TableBody>
          </Table>
        }
        const obj = asRecord(value)
        if (obj && Object.keys(obj).length > 0) {
          return <dl className="grid gap-3 sm:grid-cols-2">{Object.entries(obj).filter(([, v]) => typeof v === "string" || typeof v === "number").slice(0, 28).map(([key, data]) => <DisplayValue key={key} label={key.replaceAll("_", " ")} value={data} />)}</dl>
        }
        return <EmptyState title="No records returned" description="The service returned no displayable records." />
      }}</ResourceState>
    </CardContent>
  </Card>
}

export const recordPages: Record<string, ResourceDefinition> = {
  compute: { title: "Compute nodes", description: "Worker registrations and eligibility from Compute", path: "/v1/engineering/nodes", collectionKeys: ["nodes", "items", "results"] },
  profiles: { title: "Build profiles", description: "Stored source and execution configurations", path: "/v1/compute/profiles" },
  tasks: { title: "Tasks", description: "Canonical owner task records", path: "/v1/tasks" },
  workflows: { title: "Workflows", description: "Recorded cross-capability workflows", path: "/v1/workflows" },
  software: { title: "Software library", description: "Approved software and OCI image catalogs", path: "/v1/software" },
  activity: { title: "Activity", description: "Events derived from existing records", path: "/v1/activity?limit=100", collectionKeys: ["events"] },
  calendar: { title: "Calendar", description: "Read-only recurrence-expanded schedule", path: "/v1/calendar/occurrences", collectionKeys: ["occurrences", "events"] },
  planner: { title: "Weekly planner", description: "Advisory allocations based on recorded tasks", path: "/v1/planning/week" },
  learning: { title: "Learning", description: "Recorded knowledge and active learning sessions", path: "/v1/learning/summary" },
  health: { title: "Health", description: "Protected records from the Health service", path: "/v1/health/records" },
  recipes: { title: "Recipes", description: "Private first-party recipe library", path: "/v1/recipes" },
  pantry: { title: "Pantry", description: "Versioned ingredients and quantities", path: "/v1/pantry" },
  books: { title: "Books", description: "Private acquisition requests", path: "/v1/books/requests" },
  automation: { title: "Automations", description: "Owner-authorized recurring tasks and alerts", path: "/v1/automations" },
  security: { title: "Security", description: "Read-only security and authorization observations", path: "/v1/monitoring/security" },
}
