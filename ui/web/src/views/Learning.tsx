import { useState, type FormEvent } from "react"
import { BookOpen, ChevronRight, RefreshCcw } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { apiWrite, asRecord, list, text, useApi, type JSONRecord } from "@/lib/api"

type Session = { id: string; goal: string; target_concept_id: string; phase: string; mode: string;
  revision: number; target_level?: number; plan_version?: number; steps?: unknown[] }
type Question = { id: string; prompt: string; status: string; response_kind?: string;
  options?: { id: string; label: string }[]; kind?: string; learner_response?: string }
const transitions: Record<string, string[]> = {
  diagnose: ["plan"], plan: ["diagnose", "teach"], teach: ["plan", "practice"],
  practice: ["teach", "reassess"], reassess: ["practice", "complete", "diagnose"],
  complete: [],
}
function preview(value: unknown) {
  const obj = asRecord(value)
  if (!obj) return null
  return <div className="grid gap-2 sm:grid-cols-2">
    {Object.entries(obj).filter(([key, v]) => key !== "id" && (typeof v === "string" || typeof v === "number" || typeof v === "boolean"))
      .slice(0, 18).map(([key, value]) =>
      <div key={key} className="rounded-md border p-2 text-sm">
        <p className="text-xs text-muted-foreground">{key.replaceAll("_", " ")}</p>
        <p className="break-words">{String(value)}</p>
      </div>)}
  </div>
}

function ActiveSession({ id, csrf, revision, changed }: {
  id: string; csrf: string; revision: number; changed: () => void
}) {
  const current = useApi<Session>(`/v1/learning/sessions/${encodeURIComponent(id)}`, revision)
  const pending = useApi<unknown>(`/v1/learning/sessions/${encodeURIComponent(id)}/pending`, revision)
  const next = useApi<unknown>(`/v1/learning/sessions/${encodeURIComponent(id)}/next`, revision)
  const probe = useApi<unknown>(`/v1/learning/sessions/${encodeURIComponent(id)}/probe`, revision)
  const [answer, setAnswer] = useState("")
  const [reasoning, setReasoning] = useState("")
  const [confidence, setConfidence] = useState("0.5")
  const [assisted, setAssisted] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const session = current.data
  const qValue = asRecord(pending.data)
  const question = (asRecord(qValue?.question) ?? qValue) as Question | null
  const openQuestion = question && question.id && question.prompt && question.status !== "graded"
  async function act(path: string, payload: unknown) {
    setError(""); setBusy(true)
    try { await apiWrite("POST", path, csrf, payload); changed() }
    catch (e) { setError(e instanceof Error ? e.message : "Learning action rejected") }
    finally { setBusy(false) }
  }
  return <div className="space-y-4">
    <Card>
      <CardHeader>
        <CardTitle>{session?.goal ?? "Learning session"}</CardTitle>
        <CardDescription>Session {id} · revisions fence changes; pending assessments must be resolved before advancing.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {current.error && <p role="alert" className="text-sm text-destructive">{current.error}</p>}
        {session && <>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Badge variant="outline">{session.phase}</Badge>
            <Badge variant="secondary">{session.mode}</Badge>
            <span>Target: {session.target_concept_id}</span>
            <span>Revision {session.revision}</span>
          </div>
          <div className="flex flex-wrap gap-2">
            {session.phase === "plan" && <Button disabled={busy || !csrf} variant="outline" onClick={() => act(`/v1/learning/sessions/${id}/plan`, { expected_revision: session.revision })}>Build prerequisite plan</Button>}
            {(transitions[session.phase] ?? []).map(phase => <Button key={phase} size="sm" variant="outline" disabled={busy || !csrf} onClick={() => act(`/v1/learning/sessions/${id}/phase`, { expected_revision: session.revision, next_phase: phase })}>
              <ChevronRight aria-hidden="true" /> {phase}
            </Button>)}
          </div>
          {(session.steps ?? []).length > 0 && <div className="space-y-1">
            <p className="text-sm font-medium">Reasoning plan</p>
            {(session.steps ?? []).map((step, i) => {
              const s = asRecord(step)
              return <p key={text(s ?? {}, "id") || i} className="rounded border p-2 text-xs">{text(s ?? {}, "statement", "conclusion", "concept_id", "id") || "Recorded reasoning step"}</p>
            })}
          </div>}
        </>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      </CardContent>
    </Card>
    <Card>
      <CardHeader><CardTitle>Current assessment</CardTitle><CardDescription>Responses are recorded verbatim. Correctness, reasoning quality and mastery remain separate from the learner's self-confidence.</CardDescription></CardHeader>
      <CardContent className="space-y-3">
        {pending.error && <p role="alert" className="text-sm text-destructive">{pending.error}</p>}
        {openQuestion ? <>
          <p className="text-sm font-medium">{question.prompt}</p>
          {question.options && <div className="space-y-1">{question.options.map(option => <p className="text-sm" key={option.id}>{option.id}. {option.label}</p>)}</div>}
          {question.status === "pending" || question.status === "presented" ? <form className="space-y-3" onSubmit={e => {
            e.preventDefault()
            void act(`/v1/learning/questions/${question.id}/answer`, {
              learner_response: answer, learner_reasoning: reasoning,
              self_confidence: Number(confidence), assisted,
            })
          }}>
            <label className="block space-y-1 text-sm">Your answer
              <Textarea required maxLength={12000} value={answer} onChange={e => setAnswer(e.target.value)} />
            </label>
            <label className="block space-y-1 text-sm">Your reasoning
              <Textarea maxLength={16000} value={reasoning} onChange={e => setReasoning(e.target.value)} />
            </label>
            <div className="flex flex-wrap items-center gap-3">
              <label className="text-sm">Confidence (0–1)
                <Input className="mt-1 w-24" type="number" min={0} max={1} step={0.1} value={confidence} onChange={e => setConfidence(e.target.value)} />
              </label>
              <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={assisted} onChange={e => setAssisted(e.target.checked)} /> Received assistance</label>
              <Button disabled={busy || !csrf || !answer.trim()}>Submit response</Button>
            </div>
          </form> : <p className="text-sm text-muted-foreground">Response recorded; awaits independent grading before the next question.</p>}
        </> : <p className="text-sm text-muted-foreground">No unresolved assessment is available in this session.</p>}
      </CardContent>
    </Card>
    <div className="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader><CardTitle>Next supported action</CardTitle><CardDescription>Computed from the recorded session state; not a generated teaching claim.</CardDescription></CardHeader>
        <CardContent>{next.error ? <p role="alert" className="text-sm text-destructive">{next.error}</p> : preview(next.data) ?? <p className="text-sm text-muted-foreground">No action returned.</p>}</CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Prerequisite diagnostic</CardTitle><CardDescription>A recommendation is not a graded question or evidence of mastery.</CardDescription></CardHeader>
        <CardContent>{probe.error ? <p role="alert" className="text-sm text-destructive">{probe.error}</p> : preview(probe.data) ?? <p className="text-sm text-muted-foreground">No prerequisite probe returned.</p>}</CardContent>
      </Card>
    </div>
  </div>
}

export function Learning({ revision, csrf, refresh }: { revision: number; csrf: string; refresh: () => void }) {
  const [local, setLocal] = useState(0)
  const [selected, setSelected] = useState("")
  const [goal, setGoal] = useState("")
  const [concept, setConcept] = useState("")
  const [identifier, setIdentifier] = useState("")
  const [conceptTitle, setConceptTitle] = useState("")
  const [conceptSummary, setConceptSummary] = useState("")
  const [conceptDomain, setConceptDomain] = useState("")
  const [mode, setMode] = useState("learn")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [graph, setGraph] = useState<unknown>(null)
  const [graphError, setGraphError] = useState("")
  const [focus, setFocus] = useState("")
  const key = revision + local
  const summary = useApi<unknown>("/v1/learning/summary", key)
  const sessions = useApi<unknown>("/v1/learning/sessions", key)
  const reviews = useApi<unknown>("/v1/learning/reviews", key)
  const sessionRows = list(sessions.data, ["sessions", "items", "results"]) as unknown as Session[]
  const queue = list(reviews.data, ["items", "queue", "reviews", "due", "results"])
  function changed() { setLocal(v => v + 1); refresh() }
  async function loadGraph() {
    setGraphError(""); setBusy(true)
    try { setGraph(await apiWrite("POST", "/v1/learning/app-tools/get_graph/call", csrf, { focus, depth: 2, limit: 60 })) }
    catch (e) { setGraphError(e instanceof Error ? e.message : "Graph unavailable") }
    finally { setBusy(false) }
  }
  const nodes = list(graph, ["nodes", "concepts", "items", "results"])
  async function createSession(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setError(""); setBusy(true)
    try {
      const created = await apiWrite<unknown>("POST", "/v1/learning/sessions", csrf, { goal: goal.trim(), target_concept_id: concept.trim(), mode })
      const row = asRecord(created)
      setSelected(text(row ?? {}, "id")); setGoal(""); changed()
    } catch (e) { setError(e instanceof Error ? e.message : "Unable to start the session") }
    finally { setBusy(false) }
  }
  return <div className="space-y-5">
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><BookOpen className="size-4" aria-hidden="true" /> Learning</CardTitle>
        <CardDescription>Foundational understanding, independent problem-solving and delayed retention are measured separately. Tutoring responses or uploaded material do not constitute mastery.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {summary.error ? <p role="alert" className="text-sm text-destructive">{summary.error}</p> :
          summary.loading ? <p role="status" className="text-sm">Loading learning state…</p> : preview(summary.data)}
        <p className="text-xs text-muted-foreground">Assessments must be evaluated against their committed answer keys and reasoning rubrics. A model-generated suggestion is not a verified learning outcome.</p>
      </CardContent>
    </Card>
    <div className="grid gap-5 lg:grid-cols-2">
      <Card>
        <CardHeader><CardTitle>Concept graph</CardTitle><CardDescription>Explore registered concepts to choose an actual target for a learning session.</CardDescription></CardHeader>
        <CardContent className="space-y-3">
          <div className="flex gap-2"><Input aria-label="Concept graph focus" value={focus} onChange={e => setFocus(e.target.value)} placeholder="Concept ID (optional)" />
            <Button variant="outline" disabled={busy || !csrf} onClick={loadGraph}><RefreshCcw aria-hidden="true" /> Explore</Button></div>
          {graphError && <p role="alert" className="text-sm text-destructive">{graphError}</p>}
          <div className="max-h-64 space-y-2 overflow-y-auto">
            {nodes.map((n, i) => <button key={text(n, "id") || i} type="button" className="w-full rounded border p-2 text-left text-sm hover:bg-accent"
              onClick={() => setConcept(text(n, "id"))}>
              <span className="font-medium">{text(n, "label", "name", "title", "id")}</span>
              <span className="block break-all text-xs text-muted-foreground">{text(n, "id")}</span>
            </button>)}
          </div>
          {graph !== null && nodes.length === 0 && <p className="text-xs text-muted-foreground">No concepts in this graph selection.</p>}
          <form className="space-y-2 border-t pt-3" onSubmit={async e => {
            e.preventDefault(); setError(""); setBusy(true)
            try {
              await apiWrite("POST", "/v1/learning/concepts", csrf, {
                identifier, title: conceptTitle.trim(), summary: conceptSummary.trim(), domain: conceptDomain.trim(),
              })
              setConcept(identifier); setIdentifier(""); setConceptTitle(""); setConceptSummary(""); setConceptDomain("")
              setGraph(null); changed()
            } catch (e) { setGraphError(e instanceof Error ? e.message : "Concept registration failed") }
            finally { setBusy(false) }
          }}>
            <p className="text-sm font-medium">Register a curriculum concept</p>
            <label className="block space-y-1 text-sm">Identifier (lowercase and hyphens)
              <Input required maxLength={128} pattern="[a-z][a-z0-9]*(-[a-z0-9]+)*"
                value={identifier} onChange={e => setIdentifier(e.target.value)} placeholder="fourier-transform" />
            </label>
            <label className="block space-y-1 text-sm">Title
              <Input required maxLength={500} value={conceptTitle} onChange={e => setConceptTitle(e.target.value)} />
            </label>
            <label className="block space-y-1 text-sm">Summary
              <Textarea required maxLength={4000} value={conceptSummary} onChange={e => setConceptSummary(e.target.value)} />
            </label>
            <label className="block space-y-1 text-sm">Domain
              <Input maxLength={100} value={conceptDomain} onChange={e => setConceptDomain(e.target.value)} />
            </label>
            <Button disabled={busy || !csrf || !identifier || !conceptTitle.trim() || !conceptSummary.trim()}>Register concept</Button>
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Start learning session</CardTitle><CardDescription>Choose a registered concept. A new session begins without awarding mastery.</CardDescription></CardHeader>
        <CardContent>
          <form className="space-y-3" onSubmit={createSession}>
            <label className="block space-y-1 text-sm">Learning objective
              <Textarea required value={goal} maxLength={500} onChange={e => setGoal(e.target.value)} />
            </label>
            <label className="block space-y-1 text-sm">Target concept ID
              <Input required maxLength={128} value={concept} onChange={e => setConcept(e.target.value)} />
            </label>
            <label className="block space-y-1 text-sm">Mode
              <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={mode} onChange={e => setMode(e.target.value)}>
                <option value="learn">Learn</option><option value="practise">Practise</option>
                <option value="solve">Independent solve</option><option value="assess">Assess</option>
              </select>
            </label>
            <Button disabled={busy || !csrf || !goal.trim() || !concept.trim()}>Start session</Button>
            {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          </form>
        </CardContent>
      </Card>
    </div>
    <Card>
      <CardHeader><CardTitle>Active learning sessions</CardTitle><CardDescription>Resume any incomplete session; diagnostic answers and progress remain in the Learning service.</CardDescription></CardHeader>
      <CardContent className="space-y-2">
        {sessions.error && <p role="alert" className="text-sm text-destructive">{sessions.error}</p>}
        {sessionRows.length === 0 && !sessions.loading && !sessions.error && <p className="text-sm text-muted-foreground">No active sessions.</p>}
        {sessionRows.map(s => <button key={s.id} type="button" onClick={() => setSelected(s.id)}
          className={`flex w-full items-center justify-between gap-2 rounded border p-3 text-left text-sm hover:bg-accent ${selected === s.id ? "border-primary" : ""}`}>
          <span className="font-medium">{s.goal}</span><Badge variant="secondary">{s.phase}</Badge>
        </button>)}
      </CardContent>
    </Card>
    {selected && <ActiveSession key={selected} id={selected} revision={key} csrf={csrf} changed={changed} />}
    <Card>
      <CardHeader><CardTitle>Delayed review queue</CardTitle><CardDescription>Only confirmed evidence can satisfy retention. The queue records obligations; it does not silently create a new task or award mastery.</CardDescription></CardHeader>
      <CardContent className="space-y-2">
        {reviews.error && <p role="alert" className="text-sm text-destructive">{reviews.error}</p>}
        {queue.length === 0 && !reviews.error && !reviews.loading && <p className="text-sm text-muted-foreground">No delayed reviews currently returned.</p>}
        {queue.map((item: JSONRecord, i) => <div key={text(item, "id", "concept_id") || i} className="flex flex-wrap justify-between gap-2 rounded border p-3 text-sm">
          <span>{text(item, "title", "label", "concept_id", "id") || "Review obligation"}</span>
          <Badge variant="secondary">{text(item, "due_at", "due", "status") || "Pending"}</Badge>
        </div>)}
      </CardContent>
    </Card>
  </div>
}
