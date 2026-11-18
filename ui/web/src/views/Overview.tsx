import { Activity, Cpu, FolderKanban, ListTodo, Server } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { ResourceState, EmptyState } from "@/components/ResourceState"
import { list, text, statusVariant, useApi, type JSONRecord } from "@/lib/api"

function Metric({ title, count, description, icon: Icon }: {
  title: string
  count: number | null
  description: string
  icon: typeof Server
}) {
  return <Card className="gap-4">
    <CardHeader className="flex flex-row items-center justify-between">
      <CardDescription>{title}</CardDescription>
      <Icon className="size-4 text-muted-foreground" aria-hidden="true" />
    </CardHeader>
    <CardContent>
      <p className="text-3xl font-semibold tabular-nums">{count === null ? "—" : count}</p>
      <p className="mt-2 text-xs text-muted-foreground">{description}</p>
    </CardContent>
  </Card>
}

function Preview({ title, entries, empty }: { title: string; entries: JSONRecord[]; empty: string }) {
  return <Card>
    <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
    <CardContent className="space-y-3">
      {entries.length === 0 ? <EmptyState title={empty} /> :
        entries.slice(0, 6).map((item, i) =>
          <div key={text(item, "id", "name", "title") || i} className="flex items-center justify-between gap-3 border-b pb-3 last:border-0 last:pb-0">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{text(item, "title", "name", "id") || "Untitled"}</p>
              <p className="truncate text-xs text-muted-foreground">{text(item, "role", "description", "kind", "notes")}</p>
            </div>
            {text(item, "status", "mode") && <Badge variant={statusVariant(text(item, "status", "mode"))}>{text(item, "status", "mode")}</Badge>}
          </div>)
      }
    </CardContent>
  </Card>
}

export function Overview({ revision }: { revision: number }) {
  const nodes = useApi<unknown>("/v1/nodes", revision)
  const projects = useApi<unknown>("/v1/engineering/projects", revision)
  const tasks = useApi<unknown>("/v1/tasks", revision)
  const runs = useApi<unknown>("/v1/engineering/runs", revision)

  const nodeRows = nodes.data === null ? [] : list(nodes.data)
  const projectRows = projects.data === null ? [] : list(projects.data, ["projects", "items", "results"])
  const taskRows = tasks.data === null ? [] : list(tasks.data)
  const runRows = runs.data === null ? [] : list(runs.data, ["runs", "items", "results"])

  return <div className="space-y-6">
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Metric title="Declared nodes" count={nodes.error || nodes.loading ? null : nodeRows.length} description="Inventory, not a live readiness guarantee" icon={Server} />
      <Metric title="Projects" count={projects.error || projects.loading ? null : projectRows.length} description="Project catalog records" icon={FolderKanban} />
      <Metric title="Open tasks" count={tasks.error || tasks.loading ? null : taskRows.filter(x => text(x, "status") === "open").length} description="Recorded personal tasks" icon={ListTodo} />
      <Metric title="Engineering runs" count={runs.error || runs.loading ? null : runRows.length} description="Durable run records" icon={Cpu} />
    </div>
    <div className="grid gap-4 lg:grid-cols-2">
      <ResourceState value={nodes}>{() => <Preview title="Nodes" entries={nodeRows} empty="No nodes declared" />}</ResourceState>
      <ResourceState value={tasks}>{() => <Preview title="Open tasks" entries={taskRows.filter(x => text(x, "status") === "open")} empty="No open tasks" />}</ResourceState>
      <ResourceState value={projects}>{() => <Preview title="Research projects" entries={projectRows} empty="No projects returned" />}</ResourceState>
      <ResourceState value={runs}>{() => <Preview title="Engineering activity" entries={runRows} empty="No recorded runs" />}</ResourceState>
    </div>
    <div className="flex items-center gap-2 text-xs text-muted-foreground"><Activity aria-hidden="true" className="size-4" /> Values reflect the most recent authenticated API response.</div>
  </div>
}
