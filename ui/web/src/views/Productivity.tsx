import { useState } from "react"
import { Check, ListTodo, Plus, RefreshCcw } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { apiWrite, list, useApi } from "@/lib/api"

type Task = {
  id: string; title: string; notes?: string; status: string; revision: number;
  minutes: number; priority: number; energy?: string; location?: string;
  due?: string; scheduled?: string; recurrence?: string; goal_id?: string;
  parent_id?: string; depends_on?: string[]; decomposed?: boolean
}
type Goal = { id: string; title: string; notes?: string }
type WeeklyReport = { week_of: string; total_completed: number; minimum_daily_tasks: number;
  days: { date: string; completed: number; minimum_met: boolean }[];
  goals: { goal_id: string; title: string; tasks: number; completed: number }[]; note?: string }
type WeeklyPlan = { from: string; until: string; slots: {
  task_id: string; title: string; start: string; end: string; minutes: number;
  deadline_status?: string
}[]; unplanned: { task_id: string; title: string; reason: string }[];
  limitations: string[] }
type Review = { entries: { task_id: string; title: string; category: string; due?: string }[]; note?: string }
type TaskForm = {
  title: string; notes: string; goal_id: string; minutes: string; priority: string;
  due: string; recurrence: string; energy: string; location: string
}
const emptyForm: TaskForm = { title: "", notes: "", goal_id: "", minutes: "25",
  priority: "2", due: "", recurrence: "", energy: "", location: "" }

function taskPayload(f: TaskForm) {
  return {
    title: f.title.trim(), notes: f.notes.trim(), goal_id: f.goal_id,
    minutes: Number(f.minutes), priority: Number(f.priority), due: f.due,
    recurrence: f.recurrence, energy: f.energy, location: f.location,
  }
}
function dateOnly(value: string) { return value ? value.slice(0, 10) : "" }
function TaskFields({ value, update, goals }: {
  value: TaskForm; update: (field: keyof TaskForm, value: string) => void; goals: Goal[]
}) {
  return <div className="grid gap-3 sm:grid-cols-2">
    <label className="space-y-1 text-sm sm:col-span-2">Title
      <Input required maxLength={500} value={value.title} onChange={e => update("title", e.target.value)} />
    </label>
    <label className="space-y-1 text-sm sm:col-span-2">Notes
      <Textarea value={value.notes} onChange={e => update("notes", e.target.value)} />
    </label>
    <label className="space-y-1 text-sm">Estimated minutes
      <Input type="number" min={0} max={1440} value={value.minutes} onChange={e => update("minutes", e.target.value)} />
    </label>
    <label className="space-y-1 text-sm">Priority (0–5)
      <Input type="number" min={0} max={5} value={value.priority} onChange={e => update("priority", e.target.value)} />
    </label>
    <label className="space-y-1 text-sm">Due date
      <Input type="date" value={value.due} onChange={e => update("due", e.target.value)} />
    </label>
    <label className="space-y-1 text-sm">Recurrence
      <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={value.recurrence} onChange={e => update("recurrence", e.target.value)}>
        <option value="">One time</option><option value="daily">Daily</option>
        <option value="weekdays">Weekdays</option><option value="weekly">Weekly</option>
        <option value="monthly">Monthly</option>
      </select>
    </label>
    <label className="space-y-1 text-sm">Effort
      <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={value.energy} onChange={e => update("energy", e.target.value)}>
        <option value="">Any</option><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option>
      </select>
    </label>
    <label className="space-y-1 text-sm">Goal
      <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={value.goal_id} onChange={e => update("goal_id", e.target.value)}>
        <option value="">Unassigned</option>{goals.map(goal => <option key={goal.id} value={goal.id}>{goal.title}</option>)}
      </select>
    </label>
    <label className="space-y-1 text-sm sm:col-span-2">Context / location
      <Input maxLength={200} value={value.location} onChange={e => update("location", e.target.value)} />
    </label>
  </div>
}

function TaskRow({ task, goals, allTasks, busy, complete, save, decompose }: {
  task: Task; goals: Goal[]; allTasks: Task[]; busy: boolean;
  complete: (id: string) => Promise<void>;
  save: (task: Task, data: TaskForm) => Promise<void>;
  decompose: (task: Task, steps: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false)
  const [breaking, setBreaking] = useState(false)
  const [breakdown, setBreakdown] = useState("")
  const [form, setForm] = useState<TaskForm>({
    title: task.title, notes: task.notes ?? "", goal_id: task.goal_id ?? "",
    minutes: String(task.minutes), priority: String(task.priority), due: dateOnly(task.due ?? ""),
    recurrence: task.recurrence ?? "", energy: task.energy ?? "", location: task.location ?? "",
  })
  const update = (key: keyof TaskForm, value: string) => setForm(f => ({ ...f, [key]: value }))
  const waiting = (task.depends_on ?? []).filter(id => !allTasks.some(t => t.id === id && t.status === "completed")).length
  const children = allTasks.filter(t => t.parent_id === task.id)
  const blocked = waiting > 0 || (task.decomposed && children.some(t => t.status !== "completed"))
  return <article className="rounded-md border p-3">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-sm font-medium">{task.title}</p>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>{task.minutes || "Unestimated"} min</span>
          <span>Priority {task.priority}</span>
          {task.due && <span>Due {dateOnly(task.due)}</span>}
          {task.recurrence && <Badge variant="secondary">{task.recurrence}</Badge>}
          {waiting > 0 && <Badge variant="secondary">{waiting} prerequisites</Badge>}
          {task.decomposed && <Badge variant="outline">Parent · {children.length} subtasks</Badge>}
        </div>
        {task.notes && <p className="text-xs text-muted-foreground">{task.notes}</p>}
      </div>
      {task.status === "open" && <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={busy} onClick={() => setEditing(!editing)}>Edit</Button>
        {!task.decomposed && <Button size="sm" variant="outline" disabled={busy} onClick={() => setBreaking(!breaking)}>Split</Button>}
        <Button size="sm" disabled={busy || blocked} title={blocked ? "Complete the prerequisites and subtasks first" : ""} onClick={() => complete(task.id)}>
          <Check aria-hidden="true" /> Complete
        </Button>
      </div>}
    </div>
    {editing && <form className="mt-3 space-y-3 border-t pt-3" onSubmit={async e => { e.preventDefault(); await save(task, form); setEditing(false) }}>
      <TaskFields value={form} update={update} goals={goals} />
      <Button disabled={busy || (form.recurrence !== "" && !form.due)}>Save task</Button>
    </form>}
    {breaking && <form className="mt-3 space-y-2 border-t pt-3" onSubmit={async e => { e.preventDefault(); await decompose(task, breakdown); setBreaking(false) }}>
      <label className="block text-sm">Subtasks, one per line as <code>Title | minutes</code>
        <Textarea className="mt-1 font-mono text-xs" value={breakdown} onChange={e => setBreakdown(e.target.value)}
          placeholder={"Review assumptions | 30\nBuild prototype | 60\nVerify result | 30"} />
      </label>
      <p className="text-xs text-muted-foreground">An atomic change creates the subtasks. Each follows the previous step as a prerequisite; the parent can finish only when its children do.</p>
      <Button disabled={busy}>Create subtasks</Button>
    </form>}
  </article>
}

export function Productivity({ revision, csrf, refresh, plannerOnly = false }: {
  revision: number; csrf: string; refresh: () => void; plannerOnly?: boolean
}) {
  const [local, setLocal] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [task, setTask] = useState<TaskForm>(emptyForm)
  const [newGoal, setNewGoal] = useState("")
  const [days, setDays] = useState("180,180,180,180,180,180,180")
  const [hour, setHour] = useState(9)
  const [reviewDays, setReviewDays] = useState(7)
  const rerender = revision + local
  const tasks = useApi<unknown>("/v1/tasks", rerender)
  const goals = useApi<unknown>("/v1/goals", rerender)
  const report = useApi<WeeklyReport>("/v1/reports/weekly", rerender)
  const plan = useApi<WeeklyPlan>(`/v1/planning/week?day_minutes=${encodeURIComponent(days)}&start_hour=${hour}`, rerender)
  const review = useApi<Review>(`/v1/planning/review?days=${reviewDays}`, rerender)
  const records = list(tasks.data, ["tasks", "items", "results"]) as unknown as Task[]
  const goalRows = list(goals.data, ["goals", "items", "results"]) as unknown as Goal[]
  const open = records.filter(t => t.status === "open")
  const done = records.filter(t => t.status === "completed")
  const blocked = new Set(open.filter(t => (t.depends_on ?? []).some(id => !done.some(d => d.id === id))).map(t => t.id))
  const update = (key: keyof TaskForm, value: string) => setTask(f => ({ ...f, [key]: value }))
  async function mutate(work: () => Promise<unknown>) {
    setError(""); setBusy(true)
    try { await work(); setLocal(n => n + 1); refresh() }
    catch (e) { setError(e instanceof Error ? e.message : "The change could not be saved"); throw e }
    finally { setBusy(false) }
  }
  async function complete(id: string) { try { await mutate(() => apiWrite("POST", `/v1/tasks/${id}/complete`, csrf, {})) } catch { /* error shown above */ } }
  async function save(t: Task, f: TaskForm) {
    try { await mutate(() => apiWrite("PUT", `/v1/tasks/${t.id}`, csrf, { ...taskPayload(f), scheduled: t.scheduled ?? "", expected_revision: t.revision })) }
    catch { /* error shown above */ }
  }
  async function decompose(t: Task, raw: string) {
    const lines = raw.split("\n").map(s => s.trim()).filter(Boolean)
    const steps = lines.map((line, i) => {
      const at = line.lastIndexOf("|")
      const title = line.slice(0, at).trim()
      const minutes = Number(line.slice(at + 1).trim())
      if (at <= 0 || !title || !Number.isInteger(minutes) || minutes < 1 || minutes > 1440) throw new Error("Use Title | minutes on each line")
      return { title, minutes, depends_on_steps: i ? [i - 1] : [] }
    })
    if (steps.length < 2 || steps.length > 32) { setError("Specify 2–32 valid subtasks"); return }
    try { await mutate(() => apiWrite("POST", `/v1/tasks/${t.id}/decompose`, csrf, { expected_revision: t.revision, steps })) }
    catch { /* error shown above */ }
  }
  return <div className="space-y-5">
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><ListTodo className="size-4" /> Productivity</CardTitle>
        <CardDescription>One canonical task and goal ledger. Planning remains advisory; recorded completions determine progress. Small tasks count equally toward the daily minimum.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        <div className="flex flex-wrap gap-3 text-sm">
          <Badge variant="outline">{open.length} open tasks</Badge>
          <Badge variant="outline">{blocked.size} blocked</Badge>
          <Badge variant="outline">{report.data?.total_completed ?? "—"} completed this week</Badge>
        </div>
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {(tasks.error || goals.error) && <p role="alert" className="text-sm text-destructive">{tasks.error || goals.error}</p>}
      </CardContent>
    </Card>
    {!plannerOnly && <>
      <div className="grid gap-5 lg:grid-cols-2">
        <Card>
          <CardHeader><CardTitle>Add task</CardTitle><CardDescription>Record actionable work, with optional recurrence and a goal.</CardDescription></CardHeader>
          <CardContent>
            <form className="space-y-3" onSubmit={async e => {
              e.preventDefault()
              if (task.recurrence && !task.due) { setError("Recurring tasks require a due date"); return }
              try { await mutate(() => apiWrite("POST", "/v1/tasks", csrf, taskPayload(task))); setTask(emptyForm) } catch { /* error shown above */ }
            }}>
              <TaskFields value={task} update={update} goals={goalRows} />
              <Button disabled={busy || !csrf}><Plus aria-hidden="true" /> Create task</Button>
            </form>
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Goals and weekly record</CardTitle><CardDescription>Progress reflects completed tasks, not scheduled intentions.</CardDescription></CardHeader>
          <CardContent className="space-y-4">
            <form className="flex gap-2" onSubmit={async e => {
              e.preventDefault()
              try { await mutate(() => apiWrite("POST", "/v1/goals", csrf, { title: newGoal.trim(), notes: "" })); setNewGoal("") } catch { /* error shown above */ }
            }}>
              <Input aria-label="New goal title" maxLength={500} placeholder="New goal" required value={newGoal} onChange={e => setNewGoal(e.target.value)} />
              <Button disabled={busy || !csrf}>Add goal</Button>
            </form>
            {goalRows.map(g => <div key={g.id} className="flex items-center justify-between gap-2 rounded border p-2 text-sm">
              <span>{g.title}</span>
              <Badge variant="secondary">{records.filter(t => t.goal_id === g.id && t.status === "completed").length}/{records.filter(t => t.goal_id === g.id).length}</Badge>
            </div>)}
            {report.error ? <p role="alert" className="text-sm text-destructive">{report.error}</p> : <>
              <p className="text-sm font-medium">Week of {report.data?.week_of ?? "—"} · {report.data?.minimum_daily_tasks ?? 3} completions/day target</p>
              <div className="grid grid-cols-7 gap-1">{(report.data?.days ?? []).map(d => <div key={d.date} className="rounded border p-1 text-center text-xs" title={d.date}>
                <span className="block text-muted-foreground">{d.date.slice(5)}</span>
                <span className={d.minimum_met ? "font-semibold" : ""}>{d.completed}</span>
              </div>)}</div>
              <p className="text-xs text-muted-foreground">{report.data?.note}</p>
            </>}
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader><CardTitle>Task inbox</CardTitle><CardDescription>Blocked prerequisites and incomplete child tasks cannot be marked complete.</CardDescription></CardHeader>
        <CardContent className="space-y-3">
          {tasks.loading ? <p role="status" className="text-sm">Loading tasks…</p> :
            open.length === 0 ? <p className="text-sm text-muted-foreground">No open tasks.</p> :
            open.map(t => <TaskRow key={t.id} task={t} goals={goalRows} allTasks={records} busy={busy || !csrf} complete={complete} save={save} decompose={decompose} />)}
        </CardContent>
      </Card>
    </>}
    <Card>
      <CardHeader><CardTitle>Weekly allocation</CardTitle><CardDescription>Seven daily capacities in minutes, Monday through Sunday; 0 means no availability. External calendar events are respected only when the private calendar feed is configured and reachable.</CardDescription></CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-end gap-3">
          <label className="min-w-64 flex-1 space-y-1 text-sm">Daily capacity, seven comma-separated numbers
            <Input value={days} onChange={e => setDays(e.target.value)} />
          </label>
          <label className="w-24 space-y-1 text-sm">Start hour
            <Input type="number" min={0} max={16} value={hour} onChange={e => setHour(Number(e.target.value))} />
          </label>
          <Button variant="outline" onClick={() => setLocal(v => v + 1)}><RefreshCcw aria-hidden="true" /> Recalculate</Button>
        </div>
        {plan.error ? <p role="alert" className="text-sm text-destructive">{plan.error}</p> :
          plan.loading ? <p role="status" className="text-sm">Calculating…</p> : <>
            <div className="space-y-2">{(plan.data?.slots ?? []).map(slot => <div key={slot.task_id} className="flex flex-wrap justify-between gap-2 rounded border p-3 text-sm">
              <span>{slot.title}</span>
              <span className="text-muted-foreground">{new Date(slot.start).toLocaleString()} · {slot.minutes} min {slot.deadline_status === "late" ? "· late" : ""}</span>
            </div>)}</div>
            {(plan.data?.unplanned ?? []).map(t => <p className="text-xs text-muted-foreground" key={t.task_id}>{t.title}: {t.reason}</p>)}
            {(plan.data?.limitations ?? []).map((note, i) => <p className="text-xs text-muted-foreground" key={i}>{note}</p>)}
          </>}
      </CardContent>
    </Card>
    <Card>
      <CardHeader><CardTitle>Attention review</CardTitle><CardDescription>Recorded deadlines, missing estimates, blockers and tasks due for review. Age alone does not establish neglect.</CardDescription></CardHeader>
      <CardContent className="space-y-2">
        <label className="flex items-center gap-2 text-sm">Horizon (days)
          <Input className="w-24" type="number" min={1} max={30} value={reviewDays} onChange={e => setReviewDays(Number(e.target.value))} />
        </label>
        {review.error && <p role="alert" className="text-sm text-destructive">{review.error}</p>}
        {(review.data?.entries ?? []).map(entry => <div key={entry.task_id} className="flex justify-between gap-2 rounded border p-2 text-sm">
          <span>{entry.title}</span><Badge variant="secondary">{entry.category.replaceAll("_", " ")}</Badge>
        </div>)}
        <p className="text-xs text-muted-foreground">{review.data?.note}</p>
      </CardContent>
    </Card>
  </div>
}
