import { useState, type FormEvent } from "react"
import { CheckCircle2, FileCode2, Play } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Card, CardHeader, CardTitle, CardContent, CardDescription } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { ResourceState, EmptyState } from "@/components/ResourceState"
import { apiWrite, list, text, validProject, statusVariant, useApi, asRecord } from "@/lib/api"

const REPO = /^[A-Za-z0-9_.-]{1,100}\/[A-Za-z0-9_.-]{1,100}$/
const REVISION = /^[0-9a-f]{40}$/
const IMAGE = /^[a-z0-9][a-z0-9_-]{0,79}$/

function VerifiedRunForm({ csrf, onSubmitted }: { csrf: string; onSubmitted: () => void }) {
  const [project, setProject] = useState("")
  const [repository, setRepository] = useState("")
  const [revision, setRevision] = useState("")
  const [image, setImage] = useState("")
  const [checks, setChecks] = useState("")
  const [cpu, setCpu] = useState("1")
  const [memory, setMemory] = useState("2048")
  const [duration, setDuration] = useState("90")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [created, setCreated] = useState("")

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    setError("")
    setCreated("")
    const commands = checks.split("\n").map(v => v.trim()).filter(Boolean)
    const cpuNumber = Number(cpu), memoryNumber = Number(memory), durationNumber = Number(duration)
    if (!validProject(project) || !REPO.test(repository) || !REVISION.test(revision) ||
        !IMAGE.test(image) || commands.length === 0 || commands.length > 8 ||
        commands.some(v => v.length > 512 || /[\r\n\0]/.test(v)) ||
        !(cpuNumber >= 0.1 && cpuNumber <= 8) ||
        !Number.isInteger(memoryNumber) || !(memoryNumber >= 256 && memoryNumber <= 16384) ||
        !Number.isInteger(durationNumber) || !(durationNumber >= 10 && durationNumber <= 180)) {
      setError("Supply an exact project, repository, 40-character Git commit, approved image and bounded test commands.")
      return
    }
    if (!csrf) { setError("A verified browser session is required to submit builds."); return }
    setBusy(true)
    try {
      const payload = {
        project, repository, revision, image_pool_id: image,
        objective: "Verify the declared source revision with the approved toolchain",
        checks: commands, cpu: cpuNumber, memory_mb: memoryNumber, duration_minutes: durationNumber,
        prepare_only: true, verify_only: true,
      }
      const result = await apiWrite<unknown>("POST", "/v1/engineering/runs", csrf, payload)
      const record = asRecord(result)
      setCreated(record ? text(record, "id", "run_id") || "Request accepted" : "Request accepted")
      onSubmitted()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not submit the verification run")
    } finally {
      setBusy(false)
    }
  }

  return <Card>
    <CardHeader>
      <CardTitle className="flex items-center gap-2"><Play aria-hidden="true" className="size-4" /> Verify a source revision</CardTitle>
      <CardDescription>Run approved compiler and test commands. No source edits, authoring model, or PR publication.</CardDescription>
    </CardHeader>
    <CardContent>
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
        <label className="space-y-1 text-sm">Project ID
          <Input autoComplete="off" value={project} onChange={e => setProject(e.target.value)} required placeholder="axiom-engine" />
        </label>
        <label className="space-y-1 text-sm">Repository
          <Input autoComplete="off" value={repository} onChange={e => setRepository(e.target.value)} required placeholder="owner/repository" />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Exact source commit
          <Input className="font-mono text-xs" autoComplete="off" value={revision} onChange={e => setRevision(e.target.value)} required placeholder="40 lowercase hexadecimal characters" />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Approved image-pool ID
          <Input autoComplete="off" value={image} onChange={e => setImage(e.target.value)} required placeholder="approved-toolchain" />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Verification commands, one per line
          <Textarea className="min-h-28 font-mono text-xs" value={checks} onChange={e => setChecks(e.target.value)} required placeholder={"cargo test --locked\nlake build\ncargo build --target wasm32-unknown-unknown"} />
        </label>
        <label className="space-y-1 text-sm">CPU cores
          <Input inputMode="decimal" type="number" step="0.1" min="0.1" max="8" value={cpu} onChange={e => setCpu(e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Memory (MiB)
          <Input type="number" min="256" max="16384" value={memory} onChange={e => setMemory(e.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Maximum duration (minutes)
          <Input type="number" min="10" max="180" value={duration} onChange={e => setDuration(e.target.value)} />
        </label>
        <div className="flex items-end"><Button type="submit" disabled={busy || !csrf} className="w-full">
          <FileCode2 aria-hidden="true" /> {busy ? "Submitting…" : "Submit verification"}
        </Button></div>
        {error && <div className="sm:col-span-2"><Alert variant="destructive"><AlertTitle>Verification not submitted</AlertTitle><AlertDescription>{error}</AlertDescription></Alert></div>}
        {created && <div className="flex items-center gap-2 text-sm sm:col-span-2"><CheckCircle2 aria-hidden="true" className="size-4" /> Request: <code className="break-all">{created}</code></div>}
      </form>
    </CardContent>
  </Card>
}

function RunDetail({ id, revision }: { id: string; revision: number }) {
  const item = useApi<unknown>(`/v1/engineering/runs/${encodeURIComponent(id)}`, revision)
  return <Card>
    <CardHeader><CardTitle>Run {id}</CardTitle><CardDescription>Recorded execution, not a prediction of success</CardDescription></CardHeader>
    <CardContent><ResourceState value={item}>{result => {
      const object = asRecord(result)
      const steps = list(object?.steps)
      return steps.length === 0 ? <EmptyState title="No execution steps were returned" /> :
        <div className="space-y-2">{steps.map((step, i) => <div key={text(step, "id") || i} className="flex items-center justify-between border-b py-2">
          <span className="font-mono text-xs">{text(step, "kind", "id")}</span>
          <Badge variant={statusVariant(text(step, "status"))}>{text(step, "status") || "Unknown"}</Badge>
        </div>)}</div>
    }}</ResourceState></CardContent>
  </Card>
}

export function Engineering({ revision, csrf, refresh }: { revision: number; csrf: string; refresh: () => void }) {
  const [selected, setSelected] = useState("")
  const result = useApi<unknown>("/v1/engineering/runs", revision)
  return <div className="space-y-5">
    <VerifiedRunForm csrf={csrf} onSubmitted={refresh} />
    <Card>
      <CardHeader><CardTitle>Engineering runs</CardTitle><CardDescription>Durable, project-scoped executions</CardDescription></CardHeader>
      <CardContent><ResourceState value={result}>{value => {
        const runs = list(value, ["runs", "items", "results"])
        return runs.length === 0 ? <EmptyState title="No engineering runs returned" /> :
          <div className="space-y-2">{runs.slice(0, 40).map((run, i) => {
            const id = text(run, "id")
            return <button key={id || i} type="button" onClick={() => setSelected(id)} className="flex w-full items-center justify-between gap-3 rounded-md border p-3 text-left hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
              <div className="min-w-0"><p className="truncate font-mono text-xs">{id}</p><p className="truncate text-xs text-muted-foreground">{text(run, "project", "repository")}</p></div>
              <Badge variant={statusVariant(text(run, "status"))}>{text(run, "status") || "Unknown"}</Badge>
            </button>
          })}</div>
      }}</ResourceState></CardContent>
    </Card>
    {selected && <RunDetail id={selected} revision={revision} />}
  </div>
}
