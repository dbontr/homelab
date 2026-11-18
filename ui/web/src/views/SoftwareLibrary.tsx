import { useState } from "react"
import { ExternalLink, Package, Search, Server, Wrench } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { EmptyState, ResourceState } from "@/components/ResourceState"
import { asRecord, list, text, useApi } from "@/lib/api"

type CatalogKind = "programs" | "images" | "software"

function stateLabel(status: string): string {
  switch (status) {
    case "available": return "Approved"
    case "active": return "Approved"
    case "reviewed": return "Reviewed — not executable"
    case "requested": return "Requested"
    case "disabled": return "Disabled"
    case "retired": return "Retired"
    case "rejected": return "Rejected"
    default: return status || "Unknown"
  }
}

function stateVariant(status: string): "outline" | "secondary" | "destructive" {
  if (status === "available" || status === "active") return "outline"
  if (status === "disabled" || status === "retired" || status === "rejected") return "destructive"
  return "secondary"
}

function ProgramList({ records }: { records: ReturnType<typeof list> }) {
  if (records.length === 0) {
    return <EmptyState title="No matching programs" description="No approved or requested program matches this selection." />
  }
  return <div className="grid gap-3 lg:grid-cols-2">
    {records.map(program => {
      const status = text(program, "status")
      const source = text(program, "source_url")
      const native = text(program, "platform") !== "linux-container"
      return <div key={text(program, "id")} className="space-y-3 rounded-lg border p-4">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="min-w-0 space-y-1">
            <h3 className="font-medium">{text(program, "name")}</h3>
            <p className="font-mono text-xs text-muted-foreground">{text(program, "version")}</p>
          </div>
          <Badge variant={stateVariant(status)}>{stateLabel(status)}</Badge>
        </div>
        <p className="text-sm text-muted-foreground">{text(program, "description")}</p>
        <div className="flex flex-wrap gap-2 text-xs">
          <Badge variant="outline">{native ? text(program, "platform").replace("-", " ") : "Linux"}</Badge>
          <Badge variant="outline">{text(program, "distribution").toUpperCase()}</Badge>
          {text(program, "pool_id") && <Badge variant="outline">Pool: {text(program, "pool_id")}</Badge>}
        </div>
        {text(program, "license_notes") && <p className="text-xs text-muted-foreground">{text(program, "license_notes")}</p>}
        {text(program, "review_note") && <p className="text-xs text-muted-foreground">Review: {text(program, "review_note")}</p>}
        {source.startsWith("https://") && <a href={source} target="_blank" rel="noreferrer"
          className="inline-flex items-center gap-1 text-xs font-medium text-primary underline-offset-4 hover:underline">
          Publisher information <ExternalLink aria-hidden="true" className="size-3" />
        </a>}
      </div>
    })}
  </div>
}

function ImageList({ records }: { records: ReturnType<typeof list> }) {
  if (!records.length) return <EmptyState title="No matching images" description="Approved images appear after operator registration." />
  return <div className="space-y-2">{records.map(image => {
    const nodes = Array.isArray(image.available_nodes) ? image.available_nodes : []
    const status = text(image, "status")
    return <div key={text(image, "id")} className="rounded-lg border p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-medium">{text(image, "id")}</h3>
        <Badge variant={stateVariant(status)}>{stateLabel(status)}</Badge>
      </div>
      <p className="mt-1 break-all font-mono text-xs text-muted-foreground">{text(image, "image")}</p>
      <p className="mt-2 text-sm text-muted-foreground">
        {status === "active" && nodes.length > 0
          ? String(nodes.length) + " eligible online worker" + (nodes.length === 1 ? "" : "s")
          : "No eligible online worker"}
        {text(image, "architecture") ? " · " + text(image, "architecture") : ""}
      </p>
    </div>
  })}</div>
}

function PackageList({ records }: { records: ReturnType<typeof list> }) {
  if (!records.length) return <EmptyState title="No matching package collections" description="Operator-approved packages appear here." />
  return <div className="space-y-2">{records.map(software => {
    const packages = Array.isArray(software.packages)
      ? software.packages.filter((item): item is string => typeof item === "string") : []
    const status = text(software, "status")
    return <div key={text(software, "id")} className="rounded-lg border p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-medium">{text(software, "id")}</h3>
        <Badge variant={stateVariant(status)}>{stateLabel(status)}</Badge>
      </div>
      <p className="mt-1 text-sm text-muted-foreground">{text(software, "description")}</p>
      <p className="mt-2 text-xs text-muted-foreground">Base image: <code>{text(software, "image_id")}</code></p>
      <p className="mt-1 text-xs text-muted-foreground">Packages: {packages.join(", ") || "None returned"}</p>
    </div>
  })}</div>
}

export function SoftwareLibrary({ revision }: { revision: number }) {
  const response = useApi<unknown>("/v1/software", revision)
  const [kind, setKind] = useState<CatalogKind>("programs")
  const [search, setSearch] = useState("")
  return <div className="space-y-4">
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><Wrench aria-hidden="true" className="size-5" /> Engineering software</CardTitle>
        <CardDescription>Versioned program requests, approved environments and actual worker eligibility from Compute.</CardDescription>
      </CardHeader>
      <CardContent>
        <ResourceState value={response}>{payload => {
          const record = asRecord(payload)
          const collections: Record<CatalogKind, ReturnType<typeof list>> = {
            programs: list(record?.programs),
            images: list(record?.images),
            software: list(record?.software),
          }
          const options: Array<{ id: CatalogKind; label: string; icon: typeof Package }> = [
            { id: "programs", label: "Programs", icon: Wrench },
            { id: "images", label: "OCI images", icon: Server },
            { id: "software", label: "Packages", icon: Package },
          ]
          const normalized = search.trim().toLowerCase()
          const matching = collections[kind].filter(entry => {
            if (!normalized) return true
            const fields = [entry.id, entry.name, entry.version, entry.description, entry.status,
              entry.platform, entry.distribution, entry.image, entry.image_id]
            return fields.some(value => String(value ?? "").toLowerCase().includes(normalized))
          })
          return <div className="space-y-5">
            <div className="flex flex-wrap gap-2" role="group" aria-label="Software collection">
              {options.map(option => {
                const Icon = option.icon
                return <Button key={option.id} size="sm" variant={kind === option.id ? "default" : "outline"}
                  aria-pressed={kind === option.id} onClick={() => setKind(option.id)}>
                  <Icon aria-hidden="true" className="size-4" />
                  {option.label} ({collections[option.id].length})
                </Button>
              })}
            </div>
            <label className="flex max-w-md items-center gap-2 rounded-md border px-3">
              <Search aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
              <span className="sr-only">Search software library</span>
              <Input type="search" value={search} onChange={event => setSearch(event.target.value)}
                placeholder="Search names, versions, or platforms" className="border-0 px-0 shadow-none focus-visible:ring-0" />
            </label>
            {kind === "programs" ? <ProgramList records={matching} /> :
              kind === "images" ? <ImageList records={matching} /> : <PackageList records={matching} />}
            <p className="border-t pt-4 text-xs text-muted-foreground">
              A listed program is not necessarily installed. A reviewed program is not executable; an approved
              program still requires a compatible image, eligible worker, license and any separate hardware
              or graphical-session authorization. This page never downloads, installs or activates software.
            </p>
          </div>
        }}</ResourceState>
      </CardContent>
    </Card>
  </div>
}
