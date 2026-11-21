import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { apiWrite, useApi } from "@/lib/api"

type Anchor = {
  revision: number; active: boolean; goal_id?: string; task_id?: string;
  artifact_ref?: string; draft?: string; context?: string;
  next_action?: string; updated_at: number
}
const empty = {
  goal_id: "", task_id: "", artifact_ref: "", draft: "", context: "", next_action: ""
}
export function RecoveryPanel({ csrf, revision, refresh }: {
  csrf: string; revision: number; refresh: () => void
}) {
  const [local, setLocal] = useState(0)
  const [editing, setEditing] = useState(false)
  const [form, setForm] = useState(empty)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const anchor = useApi<Anchor>("/v1/motivation/recovery", revision + local)
  const update = (key: keyof typeof empty, value: string) =>
    setForm(previous => ({ ...previous, [key]: value }))
  async function change(work: () => Promise<unknown>) {
    setBusy(true); setError("")
    try { await work(); setLocal(n => n + 1); refresh(); setEditing(false) }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Recovery record rejected") }
    finally { setBusy(false) }
  }
  return <Card>
    <CardHeader>
      <CardTitle>Resume research</CardTitle>
      <CardDescription>Keep an owner-supplied anchor to the last draft, artifact and next action. A saved draft is not evidence that work was completed.</CardDescription>
    </CardHeader>
    <CardContent className="space-y-3">
      {anchor.error && <p role="alert" className="text-sm text-destructive">{anchor.error}</p>}
      {anchor.data?.active ? <div className="space-y-2 rounded border p-3 text-sm">
        <p className="font-medium">{anchor.data.next_action}</p>
        {anchor.data.artifact_ref && <p>Artifact: {anchor.data.artifact_ref}</p>}
        {anchor.data.context && <p className="text-muted-foreground">{anchor.data.context}</p>}
        {anchor.data.draft && <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 text-xs">{anchor.data.draft}</pre>}
      </div> : <p className="text-sm text-muted-foreground">No saved recovery anchor.</p>}
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" disabled={!csrf || busy} onClick={() => {
          const a = anchor.data
          setForm({goal_id:a?.goal_id ?? "",task_id:a?.task_id ?? "",
            artifact_ref:a?.artifact_ref ?? "",draft:a?.draft ?? "",
            context:a?.context ?? "",next_action:a?.next_action ?? ""})
          setEditing(true)
        }}>Edit recovery anchor</Button>
        <Button variant="outline" disabled={!csrf || busy || !anchor.data?.active} onClick={() => change(() =>
          apiWrite("POST","/v1/motivation/recovery/clear",csrf,{expected_revision:anchor.data?.revision ?? 0}))}>
          Clear saved anchor
        </Button>
      </div>
      {editing && <form className="grid gap-3 rounded border p-3 sm:grid-cols-2" onSubmit={async event => {
        event.preventDefault()
        await change(() => apiWrite("PUT","/v1/motivation/recovery",csrf,{
          ...form,expected_revision:anchor.data?.revision ?? 0
        }))
      }}>
        <label className="space-y-1 text-sm">Artifact reference
          <Input maxLength={512} value={form.artifact_ref} onChange={e => update("artifact_ref",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Task ID
          <Input value={form.task_id} onChange={e => update("task_id",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Goal ID
          <Input value={form.goal_id} onChange={e => update("goal_id",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Last working context
          <Textarea maxLength={1200} value={form.context} onChange={e => update("context",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Last draft (optional)
          <Textarea maxLength={4096} value={form.draft} onChange={e => update("draft",e.target.value)} />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">First action on return
          <Textarea required maxLength={500} value={form.next_action} onChange={e => update("next_action",e.target.value)} />
        </label>
        <div className="flex gap-2 sm:col-span-2">
          <Button disabled={!csrf || busy || !form.next_action.trim()}>Save resume anchor</Button>
          <Button type="button" variant="outline" onClick={() => setEditing(false)}>Cancel</Button>
        </div>
      </form>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </CardContent>
  </Card>
}
