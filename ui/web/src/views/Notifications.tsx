import { useState } from "react"
import { Bell, CheckCheck } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { apiWrite, useApi } from "@/lib/api"

type Event = {
  id: string
  source: string
  severity: "info" | "warning" | "critical"
  title: string
  message: string
  created_at: string
  acknowledged_at?: string
}
type Preference = { source: string; enabled: boolean; updated_at?: string }
type Inbox = { events: Event[] }
type Preferences = { sources: Preference[] }

export function Notifications({ revision, csrf }: { revision: number; csrf: string }) {
  const [localRevision, setLocalRevision] = useState(0)
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const feed = useApi<Inbox>("/v1/notifications", revision + localRevision)
  const preferences = useApi<Preferences>("/v1/notifications/preferences", revision + localRevision)
  const prefs = new Map((preferences.data?.sources ?? []).map(p => [p.source, p.enabled]))
  const visible = (feed.data?.events ?? []).filter(event => prefs.get(event.source) !== false)
  const unread = visible.filter(event => !event.acknowledged_at).length

  async function changeSource(source: string, enabled: boolean) {
    setError("")
    setBusy(source)
    try {
      await apiWrite("PUT", `/v1/notifications/preferences/${encodeURIComponent(source)}`, csrf, { enabled })
      setLocalRevision(r => r + 1)
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to change source preference")
    } finally { setBusy("") }
  }
  async function acknowledge(id: string) {
    setError("")
    setBusy(id)
    try {
      await apiWrite("POST", `/v1/notifications/${encodeURIComponent(id)}/ack`, csrf, {})
      setLocalRevision(r => r + 1)
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to acknowledge notification")
    } finally { setBusy("") }
  }
  return <div className="space-y-5">
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><Bell className="size-4" aria-hidden="true" /> Notification centre</CardTitle>
        <CardDescription>One durable inbox for authorized Homelab services. Muting a source hides its notices here without disabling monitoring, auditing, or event retention. External delivery is not enabled automatically.</CardDescription>
      </CardHeader>
      <CardContent>
        {feed.loading || preferences.loading ? <p role="status" className="text-sm text-muted-foreground">Loading notifications…</p> :
          feed.error || preferences.error ? <p role="alert" className="text-sm text-destructive">{feed.error ?? preferences.error}</p> :
          <p className="text-sm text-muted-foreground">{unread} unacknowledged · {visible.length} visible · {(feed.data?.events ?? []).length} retained in the current page</p>}
        {error && <p role="alert" className="mt-3 text-sm text-destructive">{error}</p>}
      </CardContent>
    </Card>
    <Card>
      <CardHeader>
        <CardTitle>Source preferences</CardTitle>
        <CardDescription>Choose which areas appear in the inbox. Producer authorization is managed separately by Security; these switches cannot grant access to publish.</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {(preferences.data?.sources ?? []).map(p => <div key={p.source} className="flex items-center justify-between gap-3 rounded-md border p-3">
            <span className="text-sm font-medium capitalize">{p.source}</span>
            <Button size="sm" variant={p.enabled ? "outline" : "secondary"}
              disabled={!!busy} aria-label={`${p.enabled ? "Hide" : "Show"} ${p.source} notifications`}
              onClick={() => changeSource(p.source, !p.enabled)}>
              {p.enabled ? "Visible" : "Muted"}
            </Button>
          </div>)}
        </div>
      </CardContent>
    </Card>
    <Card>
      <CardHeader>
        <CardTitle>Inbox</CardTitle>
        <CardDescription>Most recent 200 retained events, newest first</CardDescription>
      </CardHeader>
      <CardContent>
        {visible.length === 0 && !feed.loading && !feed.error
          ? <p className="text-sm text-muted-foreground">No notifications from visible sources. No delivery status is inferred from an empty inbox.</p>
          : <div className="space-y-3">
            {visible.map(event => <article key={event.id} className="rounded-md border p-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="font-medium">{event.title}</p>
                    <Badge variant={event.severity === "critical" ? "destructive" : "outline"}>{event.severity}</Badge>
                    <Badge variant="secondary">{event.source}</Badge>
                  </div>
                  <p className="break-words text-sm text-muted-foreground">{event.message}</p>
                  <p className="text-xs text-muted-foreground">{new Date(event.created_at).toLocaleString()}</p>
                </div>
                {event.acknowledged_at
                  ? <Badge variant="outline">Acknowledged</Badge>
                  : <Button size="sm" variant="outline" disabled={!!busy} onClick={() => acknowledge(event.id)}>
                    <CheckCheck className="size-4" aria-hidden="true" /> Acknowledge
                  </Button>}
              </div>
            </article>)}
          </div>}
      </CardContent>
    </Card>
  </div>
}
