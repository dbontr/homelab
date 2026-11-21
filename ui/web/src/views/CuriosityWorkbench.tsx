import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { apiWrite, list, useApi } from "@/lib/api"

type Task = { id: string; title: string; status: string }
type Question = {
  id: string; revision: number; enabled: boolean; paused: boolean;
  source_kind: string; source_id: string; source_label: string;
  provenance: string; question: string; worthwhile_outcome: string;
  first_action: string; expected_evidence: string
}
type State = { profile: { enabled: boolean }; curiosity_prompts: Question[] }
const empty: Question = {
  id: "",revision:0,enabled:false,paused:false,source_kind:"task",
  source_id:"",source_label:"",provenance:"owner_declared",
  question:"",worthwhile_outcome:"",first_action:"",expected_evidence:"",
}
export function CuriosityWorkbench({ csrf, revision, refresh }: {
  csrf: string; revision: number; refresh: () => void
}) {
  const [local, setLocal] = useState(0)
  const [draft, setDraft] = useState<Question>(empty)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const state = useApi<State>("/v1/motivation/state", revision + local)
  const tasks = useApi<unknown>("/v1/tasks", revision + local)
  const taskRows = list(tasks.data, ["tasks", "items", "results"]) as Task[]
  const patch = (key: keyof Question, value: string | boolean) =>
    setDraft(previous => ({ ...previous, [key]: value }))
  async function save() {
    setBusy(true); setError("")
    try {
      await apiWrite("PUT", `/v1/motivation/curiosity/${encodeURIComponent(draft.id)}`, csrf, {
        expected_revision:draft.revision, enabled:draft.enabled, paused:draft.paused,
        source_kind:draft.source_kind, source_id:draft.source_id,
        source_label:draft.source_label, question:draft.question,
        worthwhile_outcome:draft.worthwhile_outcome, first_action:draft.first_action,
        expected_evidence:draft.expected_evidence,
      })
      setDraft(empty);setLocal(n => n+1);refresh()
    }catch(failure){
      setError(failure instanceof Error ? failure.message : "Curiosity record rejected")
    }finally{setBusy(false)}
  }
  return <Card>
    <CardHeader>
      <CardTitle>Curiosity workbench</CardTitle>
      <CardDescription>Preserve worthwhile unanswered questions from real tasks or learning sources. A proposed result is a hypothesis, not evidence of a discovery.</CardDescription>
    </CardHeader>
    <CardContent className="space-y-4">
      {state.error && <p role="alert" className="text-sm text-destructive">{state.error}</p>}
      {(state.data?.curiosity_prompts ?? []).map(item => <div key={item.id} className="flex items-start justify-between gap-3 rounded border p-3 text-sm">
        <div className="space-y-1">
          <p className="font-medium">{item.question}</p>
          <p className="text-xs text-muted-foreground">{item.source_kind}:{item.source_id} · {item.provenance} · {item.enabled && !item.paused ? "Selected" : "Inactive"}</p>
          <p>First action: {item.first_action}</p>
          <p className="text-xs">Evidence to look for: {item.expected_evidence}</p>
        </div>
        <Button size="sm" variant="outline" onClick={() => setDraft(item)}>Edit</Button>
      </div>)}
      <form className="grid gap-3 sm:grid-cols-2" onSubmit={async event => {event.preventDefault();await save()}}>
        <label className="space-y-1 text-sm">Question identifier
          <Input required disabled={draft.revision>0} maxLength={80} value={draft.id} onChange={e => patch("id",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Source kind
          <select className="h-9 w-full rounded border bg-background px-2" value={draft.source_kind}
            onChange={e => setDraft(p => ({...p,source_kind:e.target.value,source_id:"",source_label:""}))}>
            <option value="task">Task</option><option value="learning">Learning concept</option><option value="research">Research source</option>
          </select>
        </label>
        {draft.source_kind==="task" ? <label className="space-y-1 text-sm sm:col-span-2">Source task
          <select className="h-9 w-full rounded border bg-background px-2" required value={draft.source_id}
            onChange={e => {const task=taskRows.find(x=>x.id===e.target.value);setDraft(p=>({...p,source_id:e.target.value,source_label:task?.title??""}))}}>
            <option value="">Choose a recorded task</option>{taskRows.map(task =>
              <option value={task.id} key={task.id}>{task.title}</option>)}
          </select>
        </label> : <>
          <label className="space-y-1 text-sm">Source identifier
            <Input required value={draft.source_id} onChange={e => patch("source_id",e.target.value)} />
          </label>
          <label className="space-y-1 text-sm">Source label
            <Input value={draft.source_label} onChange={e => patch("source_label",e.target.value)} />
          </label>
        </>}
        <label className="space-y-1 text-sm sm:col-span-2">Unanswered scholarly question
          <Textarea required value={draft.question} onChange={e => patch("question",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Worthwhile near-term outcome
          <Textarea required value={draft.worthwhile_outcome} onChange={e => patch("worthwhile_outcome",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">First bounded action
          <Textarea required value={draft.first_action} onChange={e => patch("first_action",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Evidence that would test the question
          <Textarea required value={draft.expected_evidence} onChange={e => patch("expected_evidence",e.target.value)} />
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={draft.enabled} onChange={e => patch("enabled",e.target.checked)} /> Enable voluntary question
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={draft.paused} onChange={e => patch("paused",e.target.checked)} /> Pause question
        </label>
        <div className="flex gap-2 sm:col-span-2">
          <Button disabled={!csrf || busy || !draft.id || !draft.source_id || !draft.question || !draft.first_action || !draft.worthwhile_outcome || !draft.expected_evidence}>
            {busy ? "Saving…" : "Save question"}
          </Button>
          <Button type="button" variant="outline" onClick={() => {setDraft(empty);setError("")}}>Clear</Button>
        </div>
      </form>
      <p className="text-xs text-muted-foreground">Question records do not complete tasks or award Learning mastery. Motivation must be enabled before suggestions can be requested through MCP.</p>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </CardContent>
  </Card>
}
