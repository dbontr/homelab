import { Card, CardHeader, CardTitle, CardContent, CardDescription } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { ResourceState, EmptyState } from "@/components/ResourceState"
import { list, text, useApi, asRecord, statusVariant } from "@/lib/api"

export function Monitoring({ revision }: { revision: number }) {
  const snapshot = useApi<unknown>("/v1/monitoring/summary", revision)
  const targets = useApi<unknown>("/v1/monitoring/targets", revision)
  const alerts = useApi<unknown>("/v1/monitoring/alerts", revision)
  return <div className="grid gap-4 lg:grid-cols-2">
    <Card>
      <CardHeader><CardTitle>Measurements</CardTitle><CardDescription>Prometheus measurements, not inferred host status</CardDescription></CardHeader>
      <CardContent><ResourceState value={snapshot}>{result => {
        const record = asRecord(result)
        const measurements = list(record?.metrics, ["metrics"])
        return measurements.length === 0 ? <EmptyState title="No measurements returned" /> :
          <div className="grid gap-3 sm:grid-cols-2">
            {measurements.slice(0, 16).map((m, i) =>
              <div key={text(m, "key") || i} className="rounded-lg border p-3">
                <p className="text-sm font-medium">{text(m, "label", "key")}</p>
                <p className="mt-1 text-xs text-muted-foreground">{text(m, "unit")} · {list(m.series, ["series"]).length} series</p>
              </div>)}
          </div>
      }}</ResourceState></CardContent>
    </Card>
    <Card>
      <CardHeader><CardTitle>Targets</CardTitle><CardDescription>Most recent exporter health checks</CardDescription></CardHeader>
      <CardContent><ResourceState value={targets}>{result => {
        const records = list(result, ["targets", "items"])
        return records.length === 0 ? <EmptyState title="No monitoring targets returned" /> :
          <div className="space-y-3">{records.slice(0, 30).map((t, i) =>
            <div key={text(t, "job", "instance") + i} className="flex items-center justify-between gap-3 border-b pb-3 last:border-0">
              <div className="min-w-0"><p className="truncate text-sm font-medium">{text(t, "job", "instance")}</p>
              <p className="truncate text-xs text-muted-foreground">{text(t, "instance", "last_error")}</p></div>
              <Badge variant={statusVariant(text(t, "health"))}>{text(t, "health") || "Unknown"}</Badge>
            </div>)}</div>
      }}</ResourceState></CardContent>
    </Card>
    <Card className="lg:col-span-2">
      <CardHeader><CardTitle>Alerts</CardTitle><CardDescription>Only reported alert-manager states are displayed</CardDescription></CardHeader>
      <CardContent><ResourceState value={alerts}>{result => {
        const rows = list(result, ["alerts", "items"])
        return rows.length === 0 ? <EmptyState title="No alerts returned" description="This view does not infer system health from an empty response." /> :
          <div className="space-y-3">{rows.map((alert, i) =>
            <div key={text(alert, "name") || i} className="rounded-lg border p-3">
              <div className="flex items-center gap-2"><p className="font-medium">{text(alert, "name")}</p><Badge variant={statusVariant(text(alert, "state"))}>{text(alert, "state") || "Unknown"}</Badge></div>
              <p className="mt-1 text-sm text-muted-foreground">{text(alert, "summary")}</p>
            </div>)}</div>
      }}</ResourceState></CardContent>
    </Card>
  </div>
}
