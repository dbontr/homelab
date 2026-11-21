import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { apiWrite, list, useApi } from "@/lib/api"

type Rule = {
  key: string; text: string; kind: string; revision: number; enabled: boolean; paused: boolean;
  priority: number; scope: string; goal_id?: string; context?: string; effect?: string;
  value?: string; exceptions?: string[]; expires_at?: string
}
type Goal = { id: string; title: string }
type Recommendation = { task: { id: string; title: string }; rationale: string[] }
type Advice = { recommendations: Recommendation[]; excluded: {
  task_id: string; rule_keys: string[]; explanation: string
}[]; advisory: boolean }
type FormState = {
  key: string; text: string; kind: string; revision: number; enabled: boolean;
  paused: boolean; priority: number; scope: string; goal_id: string; context: string;
  effect: string; value: string; exceptions: string; expires_at: string
}
const empty: FormState = {
  key: "", text: "", kind: "constraint", revision: 0, enabled: true,
  paused: false, priority: 5, scope: "global", goal_id: "", context: "",
  effect: "max_minutes", value: "30", exceptions: "", expires_at: "",
}

export function OwnerRules({ csrf, revision, refresh, goals }: {
  csrf: string; revision: number; refresh: () => void; goals: Goal[]
}) {
  const [local, setLocal] = useState(0)
  const [form, setForm] = useState<FormState>(empty)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const rules = useApi<unknown>("/v1/rules", revision + local)
  const advice = useApi<Advice>("/v1/recommendations?minutes=120&limit=5", revision + local)
  const rows = list(rules.data, ["rules", "items", "results"]) as Rule[]
  const update = (field: keyof FormState, value: string | number | boolean) => {
    setForm(previous => ({ ...previous, [field]: value }))
  }
  const edit = (rule: Rule) => setForm({
    key: rule.key, text: rule.text, kind: rule.kind, revision: rule.revision,
    enabled: rule.enabled, paused: rule.paused, priority: rule.priority,
    scope: rule.scope || "global", goal_id: rule.goal_id || "",
    context: rule.context || "", effect: rule.effect || "",
    value: rule.value || "", exceptions: (rule.exceptions ?? []).join(", "),
    expires_at: rule.expires_at || "",
  })
  async function save() {
    setBusy(true); setError("")
    try {
      await apiWrite("PUT", `/v1/rules/${encodeURIComponent(form.key)}/policy`, csrf, {
        expected_revision: form.revision, text: form.text.trim(), kind: form.kind,
        enabled: form.enabled, paused: form.paused, priority: form.priority,
        scope: form.scope,
        goal_id: form.scope === "goal" ? form.goal_id : "",
        context: form.scope === "context" ? form.context : "",
        effect: form.effect, value: form.effect ? form.value.trim() : "",
        exceptions: form.exceptions.split(",").map(item => item.trim()).filter(Boolean),
        expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : "",
      })
      setForm(empty); setLocal(n => n + 1); refresh()
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "The rule could not be saved")
    } finally { setBusy(false) }
  }
  return <Card>
    <CardHeader>
      <CardTitle>Personal rules</CardTitle>
      <CardDescription>Explicit constraints guide suggestions but never modify or complete your tasks. Pause, revise, or make an exception at any time.</CardDescription>
    </CardHeader>
    <CardContent className="space-y-4">
      {rules.error && <p role="alert" className="text-sm text-destructive">{rules.error}</p>}
      {rows.map(rule => <div key={rule.key} className="flex items-start justify-between gap-3 rounded border p-2 text-sm">
        <div><p className="font-medium">{rule.key} · revision {rule.revision}</p>
          <p className="text-muted-foreground">{rule.text}</p>
          <p className="text-xs">{rule.enabled && !rule.paused ? "Active" : "Inactive"} · {rule.effect || "Advisory only"}</p>
        </div>
        <Button size="sm" variant="outline" onClick={() => edit(rule)}>Edit</Button>
      </div>)}
      <form className="grid gap-3 sm:grid-cols-2" onSubmit={async event => { event.preventDefault(); await save() }}>
        <label className="space-y-1 text-sm">Identifier
          <Input required maxLength={80} disabled={form.revision > 0} value={form.key} onChange={e => update("key", e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Priority
          <Input type="number" min={0} max={10} value={form.priority} onChange={e => update("priority", Number(e.target.value))} />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Personal principle
          <Textarea required value={form.text} onChange={e => update("text", e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Kind
          <select className="h-9 w-full rounded border bg-background px-2" value={form.kind}
            onChange={e => { update("kind", e.target.value); update("effect", e.target.value === "preference" ? "prefer_goal" : "max_minutes") }}>
            <option value="constraint">Constraint</option><option value="preference">Preference</option>
          </select>
        </label>
        <label className="space-y-1 text-sm">Scope
          <select className="h-9 w-full rounded border bg-background px-2" value={form.scope} onChange={e => update("scope", e.target.value)}>
            <option value="global">Global</option><option value="goal">Goal</option><option value="context">Context</option>
          </select>
        </label>
        {form.scope === "goal" && <label className="space-y-1 text-sm">Goal
          <select className="h-9 w-full rounded border bg-background px-2" value={form.goal_id} onChange={e => update("goal_id", e.target.value)}>
            <option value="">Choose goal</option>{goals.map(goal => <option key={goal.id} value={goal.id}>{goal.title}</option>)}
          </select>
        </label>}
        {form.scope === "context" && <label className="space-y-1 text-sm">Context
          <Input required value={form.context} onChange={e => update("context", e.target.value)} />
        </label>}
        <label className="space-y-1 text-sm">Executable policy
          <select className="h-9 w-full rounded border bg-background px-2" value={form.effect} onChange={e => update("effect", e.target.value)}>
            <option value="">Advisory only</option>
            {form.kind === "constraint" ? <>
              <option value="max_minutes">Maximum task minutes</option>
              <option value="require_energy">Require effort level</option>
              <option value="exclude_energy">Exclude effort level</option>
              <option value="exclude_location">Exclude location</option>
            </> : <>
              <option value="prefer_goal">Prefer goal</option><option value="prefer_location">Prefer location</option>
            </>}
          </select>
        </label>
        <label className="space-y-1 text-sm">Policy value
          <Input value={form.value} disabled={!form.effect} onChange={e => update("value", e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Exception task identifiers
          <Input value={form.exceptions} placeholder="Comma-separated" onChange={e => update("exceptions", e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Expires at (optional)
          <Input type="datetime-local" value={form.expires_at ? form.expires_at.slice(0,16) : ""} onChange={e => update("expires_at", e.target.value)} />
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={form.enabled} onChange={e => update("enabled", e.target.checked)} /> Enabled
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={form.paused} onChange={e => update("paused", e.target.checked)} /> Paused
        </label>
        <div className="flex gap-2 sm:col-span-2">
          <Button disabled={busy || !csrf || !form.key || !form.text}>{busy ? "Saving…" : "Save rule"}</Button>
          <Button type="button" variant="outline" onClick={() => { setForm(empty); setError("") }}>Clear</Button>
        </div>
        {error && <p role="alert" className="text-sm text-destructive sm:col-span-2">{error}</p>}
      </form>
      <div className="space-y-2 border-t pt-3">
        <p className="text-sm font-medium">Explainable recommendations</p>
        {advice.error && <p role="alert" className="text-sm text-destructive">{advice.error}</p>}
        {(advice.data?.recommendations ?? []).map(item => <div className="rounded border p-2 text-sm" key={item.task.id}>
          <p className="font-medium">{item.task.title}</p>
          <p className="text-xs text-muted-foreground">{item.rationale.join(" · ")}</p>
        </div>)}
        {(advice.data?.excluded ?? []).map(item => <p className="text-xs text-muted-foreground" key={item.task_id}>
          Excluded {item.task_id} by {item.rule_keys.join(", ")} — {item.explanation}
        </p>)}
      </div>
    </CardContent>
  </Card>
}
