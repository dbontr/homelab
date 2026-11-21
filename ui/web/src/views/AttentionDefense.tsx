import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { apiWrite, useApi } from "@/lib/api"

type Plan = {
  id: string; revision: number; enabled: boolean; paused: boolean;
  trigger: string; alternative: string; context: string; response: string;
  boundary: string; outcome: string; goal_id?: string; task_id?: string;
  rule_key?: string; cooldown_minutes: number
}
type Profile = {
  revision: number; enabled: boolean; proactive_enabled: boolean;
  cadence_minutes: number; holdout_weekday_utc?: number | null;
  goal_id: string; identity: string; outcome: string; next_step: string;
  intensity: string; daily_limit: number; quiet_start_utc_hour: number; quiet_end_utc_hour: number
}
type State = { profile: Profile; attention_plans: Plan[] }
const blank: Plan = {
  id: "", revision: 0, enabled: false, paused: false, trigger: "",
  alternative: "", context: "", response: "", boundary: "", outcome: "",
  goal_id: "", task_id: "", rule_key: "", cooldown_minutes: 60,
}
export function AttentionDefense({ csrf, revision, refresh }: {
  csrf: string; revision: number; refresh: () => void
}) {
  const [local, setLocal] = useState(0)
  const [profileDraft, setProfileDraft] = useState<Profile | null>(null)
  const [entry, setEntry] = useState<Plan>(blank)
  const [error, setError] = useState("")
  const [result, setResult] = useState("")
  const [busy, setBusy] = useState(false)
  const state = useApi<State>("/v1/motivation/state", revision + local)
  const update = (field: keyof Plan, value: string | boolean | number) =>
    setEntry(previous => ({ ...previous, [field]: value }))
  async function mutate(action: () => Promise<unknown>) {
    setBusy(true); setError("")
    try { await action(); setLocal(n => n + 1); refresh() }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Operation rejected") }
    finally { setBusy(false) }
  }
  return <Card>
    <CardHeader>
      <CardTitle>Attention defense</CardTitle>
      <CardDescription>Declare distractions and voluntary responses. Nothing monitors your browsing or modifies another application. A paused motivation profile suppresses responses.</CardDescription>
    </CardHeader>
    <CardContent className="space-y-3">
      {state.error && <p role="alert" className="text-sm text-destructive">{state.error}</p>}
      <p className="text-sm">{state.data?.profile.enabled ? "Motivation interventions enabled" : "Motivation interventions paused or disabled"}</p>
      <Button type="button" variant="outline" disabled={!csrf || busy || !state.data}
        onClick={() => { setProfileDraft(state.data?.profile ?? null); setError("") }}>
        Edit motivation profile
      </Button>
      {profileDraft && <form className="grid gap-3 rounded border p-3 sm:grid-cols-2" onSubmit={async event => {
        event.preventDefault()
        await mutate(() => apiWrite("PUT", "/v1/motivation/profile", csrf, {
          expected_revision: profileDraft.revision, enabled: profileDraft.enabled,
          proactive_enabled: profileDraft.proactive_enabled,
          cadence_minutes: profileDraft.cadence_minutes,
          holdout_weekday_utc: profileDraft.holdout_weekday_utc ?? null,
          goal_id: profileDraft.goal_id, identity: profileDraft.identity,
          outcome: profileDraft.outcome, next_step: profileDraft.next_step,
          intensity: profileDraft.intensity, daily_limit: profileDraft.daily_limit,
          quiet_start_utc_hour: profileDraft.quiet_start_utc_hour,
          quiet_end_utc_hour: profileDraft.quiet_end_utc_hour,
        }))
        setProfileDraft(null)
      }}>
        <p className="text-sm sm:col-span-2">Revision {profileDraft.revision}. Enabling requires an authentic identity, outcome, and next step.</p>
        {(["identity", "outcome", "next_step", "goal_id"] as const).map(field =>
          <label className="space-y-1 text-sm" key={field}>{field.replaceAll("_"," ")}
            <Input value={profileDraft[field]} onChange={e => setProfileDraft(v => v && {...v,[field]:e.target.value})} />
          </label>)}
        <label className="space-y-1 text-sm">Style
          <select className="h-9 w-full rounded border bg-background px-2" value={profileDraft.intensity}
            onChange={e => setProfileDraft(v => v && {...v,intensity:e.target.value})}>
            <option value="guided">Guided</option><option value="direct">Direct</option>
          </select>
        </label>
        {(["cadence_minutes", "daily_limit", "quiet_start_utc_hour", "quiet_end_utc_hour"] as const).map(field =>
          <label className="space-y-1 text-sm" key={field}>{field.replaceAll("_"," ")}
            <Input type="number" min={field === "cadence_minutes" ? 60 : field === "daily_limit" ? 1 : 0}
              max={field === "cadence_minutes" ? 1440 : field === "daily_limit" ? 8 : 23}
              value={profileDraft[field]} onChange={e => setProfileDraft(v => v && {...v,[field]:Number(e.target.value)})} />
          </label>)}
        <label className="space-y-1 text-sm">Intervention-free weekday (UTC)
          <select className="h-9 w-full rounded border bg-background px-2"
            value={profileDraft.holdout_weekday_utc == null ? "" : String(profileDraft.holdout_weekday_utc)}
            onChange={e => setProfileDraft(v => v && {...v,holdout_weekday_utc:e.target.value==="" ? null : Number(e.target.value)})}>
            <option value="">None</option>{["Sunday","Monday","Tuesday","Wednesday","Thursday","Friday","Saturday"].map((d,i)=>
              <option value={i} key={d}>{d}</option>)}
          </select>
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={profileDraft.enabled}
            onChange={e => setProfileDraft(v => v && {...v,enabled:e.target.checked})} />
          Enable consented interventions
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={profileDraft.proactive_enabled}
            onChange={e => setProfileDraft(v => v && {...v,proactive_enabled:e.target.checked})} />
          Enable bounded in-app scheduling
        </label>
        <div className="flex gap-2 sm:col-span-2">
          <Button disabled={!csrf || busy}>Save profile</Button>
          <Button type="button" variant="outline" onClick={() => setProfileDraft(null)}>Cancel</Button>
        </div>
      </form>}
      <Button type="button" variant="outline" disabled={busy || !csrf}
        onClick={() => mutate(() => apiWrite("POST", "/v1/motivation/pause", csrf, {}))}>
        Pause all interventions
      </Button>
      {(state.data?.attention_plans ?? []).map(plan => <div className="flex items-start justify-between gap-2 rounded border p-3 text-sm" key={plan.id}>
        <div><p className="font-medium">{plan.id} · revision {plan.revision}</p>
          <p>{plan.trigger} · {plan.context}</p>
          <p className="text-xs text-muted-foreground">{plan.response}</p>
          <p className="text-xs">{plan.enabled && !plan.paused ? "Plan active" : "Plan inactive"}</p>
        </div>
        <Button type="button" size="sm" variant="outline" onClick={() => { setEntry(plan); setResult("") }}>Edit</Button>
      </div>)}
      <form className="grid gap-3 sm:grid-cols-2" onSubmit={async event => {
        event.preventDefault()
        await mutate(() => apiWrite("PUT", `/v1/motivation/attention-plans/${encodeURIComponent(entry.id)}`, csrf, {
          expected_revision: entry.revision, enabled: entry.enabled, paused: entry.paused,
          trigger: entry.trigger.trim(), alternative: entry.alternative.trim(),
          context: entry.context.trim(), response: entry.response.trim(),
          boundary: entry.boundary.trim(), outcome: entry.outcome.trim(),
          goal_id: entry.goal_id ?? "", task_id: entry.task_id ?? "",
          rule_key: entry.rule_key ?? "", cooldown_minutes: entry.cooldown_minutes,
        }))
      }}>
        <label className="space-y-1 text-sm">Plan ID
          <Input required disabled={entry.revision > 0} maxLength={80} value={entry.id} onChange={e => update("id",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Context
          <Input required maxLength={128} value={entry.context} onChange={e => update("context",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Distraction trigger
          <Input required value={entry.trigger} onChange={e => update("trigger",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Tempting alternative
          <Input value={entry.alternative} onChange={e => update("alternative",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Chosen response
          <Textarea required value={entry.response} onChange={e => update("response",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Boundary and exceptions
          <Textarea value={entry.boundary} onChange={e => update("boundary",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Measurable outcome
          <Textarea value={entry.outcome} onChange={e => update("outcome",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Goal reference
          <Input value={entry.goal_id ?? ""} onChange={e => update("goal_id",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Task reference
          <Input value={entry.task_id ?? ""} onChange={e => update("task_id",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Rule reference
          <Input value={entry.rule_key ?? ""} onChange={e => update("rule_key",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Cooldown minutes
          <Input type="number" min={25} max={1440} value={entry.cooldown_minutes} onChange={e => update("cooldown_minutes",Number(e.target.value))} />
        </label>
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={entry.enabled} onChange={e => update("enabled",e.target.checked)} /> Opt in to plan</label>
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={entry.paused} onChange={e => update("paused",e.target.checked)} /> Pause this plan</label>
        <div className="flex gap-2 sm:col-span-2">
          <Button disabled={!csrf || busy || !entry.id || !entry.context || !entry.trigger || !entry.response}>Save plan</Button>
          <Button type="button" variant="outline" onClick={() => {setEntry(blank);setResult("")}}>Clear</Button>
          <Button type="button" variant="outline" disabled={!csrf || busy || !entry.trigger || !entry.context}
            onClick={async () => {setResult("");await mutate(async () => {
              const response = await apiWrite("POST", "/v1/motivation/attention-response", csrf,
                {trigger: entry.trigger, context: entry.context}) as Plan
              setResult(response.response)
            })}}>Read chosen response</Button>
        </div>
      </form>
      {result && <p className="rounded border p-3 text-sm">{result}</p>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </CardContent>
  </Card>
}
