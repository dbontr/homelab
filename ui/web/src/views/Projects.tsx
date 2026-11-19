import { useState } from "react"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table"
import { EmptyState, ResourceState } from "@/components/ResourceState"
import { list, text, validProject, useApi, statusVariant, type JSONRecord } from "@/lib/api"

function ProjectRecords({ project, kind, revision }: { project: string; kind: "jobs" | "workspaces" | "artifacts"; revision: number }) {
  const response = useApi<unknown>(validProject(project) ? `/v1/engineering/projects/${encodeURIComponent(project)}/${kind}` : "", revision)
  return <Card>
    <CardHeader><CardTitle className="capitalize">{kind}</CardTitle><CardDescription>Controller-owned records for this project</CardDescription></CardHeader>
    <CardContent>
      <ResourceState value={response}>{result => {
        const entries = list(result, [kind, "items", "results"])
        return entries.length === 0 ? <EmptyState title={`No ${kind} returned`} /> :
          <Table><TableHeader><TableRow><TableHead>{kind === "artifacts" ? "File" : "Identifier"}</TableHead><TableHead>{kind === "artifacts" ? "Download" : "State"}</TableHead></TableRow></TableHeader><TableBody>
            {entries.slice(0, 35).map((entry, i) => {
              const id = text(entry, "id", "job_id", "workspace_id")
              const size = typeof entry.size === "number" ? entry.size : -1
              const downloadable = kind === "artifacts" && /^[0-9a-f]{32}$/.test(id) &&
                Number.isInteger(size) && size >= 0 && size <= 512 * 1024
              return <TableRow key={id || i}>
                <TableCell className="max-w-80 truncate font-mono text-xs">{kind === "artifacts" ? text(entry, "name", "id") : id}</TableCell>
                <TableCell>{kind === "artifacts" ?
                  downloadable ?
                    <a className="text-sm underline underline-offset-2 focus-visible:outline focus-visible:outline-2" href={`/v1/engineering/projects/${encodeURIComponent(project)}/artifacts/${id}/download`} download>Download</a> :
                    <span className="text-xs text-muted-foreground">{size > 512 * 1024 ? "File exceeds 512 KiB browser limit" : "Unavailable"}</span> :
                  <Badge variant={statusVariant(text(entry, "status", "state"))}>{text(entry, "status", "state") || "Recorded"}</Badge>}
                </TableCell>
              </TableRow>
            })}</TableBody></Table>
      }}</ResourceState>
    </CardContent>
  </Card>
}

export function Projects({ revision }: { revision: number }) {
  const [query, setQuery] = useState("")
  const [selected, setSelected] = useState("")
  const data = useApi<unknown>("/v1/engineering/projects", revision)
  const entries = data.data === null ? [] : list(data.data, ["projects", "items", "results"])
  const filtered = entries.filter(item => {
    const haystack = ["title", "name", "id", "description"].map(key => text(item, key).toLowerCase()).join(" ")
    return haystack.includes(query.trim().toLowerCase())
  })
  const resolveID = (item: JSONRecord) => text(item, "id", "project_id")

  return <div className="space-y-5">
    <div className="max-w-sm"><Input aria-label="Search projects" type="search" placeholder="Filter projects" value={query} onChange={e => setQuery(e.target.value)} /></div>
    <ResourceState value={data}>{() => <Card>
      <CardHeader><CardTitle>Project directory</CardTitle><CardDescription>Research records are distinct from their Git repositories.</CardDescription></CardHeader>
      <CardContent>
        {filtered.length === 0 ? <EmptyState title="No matching projects" /> :
          <Table><TableHeader><TableRow><TableHead>Project</TableHead><TableHead>Identifier</TableHead><TableHead /></TableRow></TableHeader><TableBody>
            {filtered.map((item, i) => {
              const id = resolveID(item)
              return <TableRow key={id || i}>
                <TableCell className="font-medium">{text(item, "title", "name", "id") || id}</TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">{id}</TableCell>
                <TableCell className="text-right"><Button variant={selected === id ? "secondary" : "outline"} size="sm" disabled={!validProject(id)} onClick={() => setSelected(id)}>Inspect</Button></TableCell>
              </TableRow>
            })}</TableBody></Table>
        }
      </CardContent>
    </Card>}</ResourceState>
    {validProject(selected) && <section className="space-y-4" aria-label="Selected project">
      <h2 className="text-lg font-semibold">{selected}</h2>
      <div className="grid gap-4 xl:grid-cols-3">
        <ProjectRecords project={selected} kind="workspaces" revision={revision} />
        <ProjectRecords project={selected} kind="jobs" revision={revision} />
        <ProjectRecords project={selected} kind="artifacts" revision={revision} />
      </div>
    </section>}
  </div>
}
