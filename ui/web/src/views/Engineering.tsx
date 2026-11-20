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
function validSourcePath(value: string): boolean {
  return value.length > 0 && value.length <= 255 && !value.startsWith("/") &&
    !/[\\\0\r\n]/.test(value) &&
    value.split("/").every(part => part !== "" && part !== "." && part !== ".." && part !== ".git")
}

function VerifiedRunForm({ csrf, onSubmitted }: { csrf: string; onSubmitted: () => void }) {
  const catalog = useApi<unknown>("/v1/software")
  const images = list(asRecord(catalog.data)?.images).filter(item =>
    text(item, "status") === "active" && Array.isArray(item.available_nodes) && item.available_nodes.length > 0)
  const eligibleImageIDs = new Set(images.map(item => text(item, "id")))
  const programs = list(asRecord(catalog.data)?.programs).filter(item =>
    text(item, "status") === "available" && text(item, "pool_kind") === "image" &&
    eligibleImageIDs.has(text(item, "pool_id")))
  const [mode, setMode] = useState<"verify" | "draft">("verify")
  const [sourcePath, setSourcePath] = useState("")
  const [sourceContent, setSourceContent] = useState("")
  const [draftTitle, setDraftTitle] = useState("")
  const [draftDescription, setDraftDescription] = useState("")
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
    if (mode === "draft" && (!validSourcePath(sourcePath) ||
        sourceContent.length === 0 || new TextEncoder().encode(sourceContent).length > 24000 ||
        draftTitle.trim().length === 0 || draftTitle.length > 256 || draftDescription.length > 8192)) {
      setError("Specify a relative source path, complete file content (up to 24 KiB), and a short draft title.")
      return
    }
    if (!csrf) { setError("A verified browser session is required to submit builds."); return }
    setBusy(true)
    try {
      const payload = {
        project, repository, revision, image_pool_id: image,
        objective: "Verify the declared source revision with the approved toolchain",
        checks: commands, cpu: cpuNumber, memory_mb: memoryNumber, duration_minutes: durationNumber,
        ...(mode === "draft" ? {
          title: draftTitle.trim(), description: draftDescription,
          base_branch: "main", expected_head: revision,
          changes: { [sourcePath]: sourceContent },
        } : { prepare_only: true, verify_only: true }),
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
      <CardTitle className="flex items-center gap-2"><Play aria-hidden="true" className="size-4" /> Engineering workspaces</CardTitle>
      <CardDescription>Verify a pinned commit or submit owner-supplied full-file changes for tests, immutable artifact export and a reviewable draft pull request. No automatic merge or deployment.</CardDescription>
    </CardHeader>
    <CardContent>
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
        <fieldset className="sm:col-span-2 space-y-2">
          <legend className="text-sm font-medium">Operation</legend>
          <label className="mr-5 inline-flex items-center gap-2 text-sm">
            <input type="radio" checked={mode === "verify"} onChange={() => setMode("verify")} name="engineering-mode" />
            Verify source only
          </label>
          <label className="inline-flex items-center gap-2 text-sm">
            <input type="radio" checked={mode === "draft"} onChange={() => setMode("draft")} name="engineering-mode" />
            Edit, verify and prepare draft PR
          </label>
        </fieldset>
        <label className="space-y-1 text-sm">Project ID
          <Input autoComplete="off" value={project} onChange={e => setProject(e.target.value)} required placeholder="research-project" />
        </label>
        <label className="space-y-1 text-sm">Repository
          <Input autoComplete="off" value={repository} onChange={e => setRepository(e.target.value)} required placeholder="owner/repository" />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Exact source commit
          <Input className="font-mono text-xs" autoComplete="off" value={revision} onChange={e => setRevision(e.target.value)} required placeholder="40 lowercase hexadecimal characters" />
        </label>
        <label className="space-y-1 text-sm sm:col-span-2">Approved execution environment
          <select className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" value={image} onChange={e => setImage(e.target.value)} required>
            <option value="">Select an image available on an online worker</option>
            {programs.length > 0 && <optgroup label="Approved programs">
              {programs.map(item => <option key={text(item, "id")} value={text(item, "pool_id")}>
                {text(item, "name")} {text(item, "version")}
              </option>)}
            </optgroup>}
            <optgroup label="Available OCI images">
            {images.map(item => <option key={text(item, "id")} value={text(item, "id")}>
              {text(item, "id")} · {(item.available_nodes as unknown[]).length} worker(s)
            </option>)}
            </optgroup>
          </select>
          {images.length === 0 && <span className="block text-xs text-muted-foreground">No eligible execution image is reported. Review the <a href="#software" className="underline">software library</a> and worker admission status.</span>}
        </label>
        {mode === "draft" && <>
          <label className="space-y-1 text-sm sm:col-span-2">Repository-relative file path
            <Input value={sourcePath} onChange={e => setSourcePath(e.target.value)} required placeholder="services/core/example.go" autoComplete="off" />
          </label>
          <label className="space-y-1 text-sm sm:col-span-2">Complete file contents (not a patch)
            <Textarea className="min-h-48 font-mono text-xs" value={sourceContent} onChange={e => setSourceContent(e.target.value)} required placeholder="Full replacement contents of the selected source file" />
          </label>
          <label className="space-y-1 text-sm sm:col-span-2">Draft pull-request title
            <Input value={draftTitle} onChange={e => setDraftTitle(e.target.value)} required maxLength={256} />
          </label>
          <label className="space-y-1 text-sm sm:col-span-2">Draft description (optional)
            <Textarea value={draftDescription} onChange={e => setDraftDescription(e.target.value)} maxLength={8192} />
          </label>
          <p className="text-xs text-muted-foreground sm:col-span-2">The selected commit must still be main HEAD at publication. Every check must pass before a draft is created. The worker cannot merge or deploy.</p>
        </>}
        <label className="space-y-1 text-sm sm:col-span-2">Verification commands, one per line
          <Textarea className="min-h-28 font-mono text-xs" value={checks} onChange={e => setChecks(e.target.value)} required placeholder={"go test ./...\nmake test"} />
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
          <FileCode2 aria-hidden="true" /> {busy ? "Submitting…" : mode === "draft" ? "Verify and prepare draft" : "Submit verification"}
        </Button></div>
        {error && <div className="sm:col-span-2"><Alert variant="destructive"><AlertTitle>Engineering request not submitted</AlertTitle><AlertDescription>{error}</AlertDescription></Alert></div>}
        {created && <div className="flex items-center gap-2 text-sm sm:col-span-2"><CheckCircle2 aria-hidden="true" className="size-4" /> Request: <code className="break-all">{created}</code></div>}
      </form>
    </CardContent>
  </Card>
}

function RunDetail({ id, revision, csrf, refresh }: { id: string; revision: number; csrf: string; refresh: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  async function cancel(expectedRevision: number) {
    if (!csrf || busy) return
    setBusy(true); setError("")
    try {
      await apiWrite("POST", `/v1/engineering/runs/${encodeURIComponent(id)}/cancel`, csrf, { revision: expectedRevision })
      refresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not cancel the run")
    } finally { setBusy(false) }
  }
  const item = useApi<unknown>(`/v1/engineering/runs/${encodeURIComponent(id)}`, revision)
  return <Card>
    <CardHeader><CardTitle>Run {id}</CardTitle><CardDescription>Recorded execution, not a prediction of success</CardDescription></CardHeader>
    <CardContent><ResourceState value={item}>{result => {
      const object = asRecord(result)
      const steps = list(object?.steps)
      const status = text(object ?? {}, "status")
      const rev = Number(object?.revision)
      return steps.length === 0 ? <EmptyState title="No execution steps were returned" /> :
        <div className="space-y-2">{steps.map((step, i) => <div key={text(step, "id") || i} className="flex flex-wrap items-center justify-between gap-2 border-b py-2">
          <span className="font-mono text-xs">{text(step, "kind", "id")}</span>
          <Badge variant={statusVariant(text(step, "status"))}>{text(step, "status") || "Unknown"}</Badge>
          {text(step, "error") && <span className="w-full text-xs text-destructive">{text(step, "error")}</span>}
          {text(asRecord(asRecord(step)?.result) ?? {}, "url") && <a className="w-full text-xs underline" href={text(asRecord(asRecord(step)?.result) ?? {}, "url")} target="_blank" rel="noopener noreferrer">Open draft pull request</a>}
        </div>)}
        {(status === "pending" || status === "running") && Number.isInteger(rev) && csrf &&
          <Button type="button" variant="outline" disabled={busy} onClick={() => cancel(rev)}>{busy ? "Cancelling…" : "Cancel run"}</Button>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        </div>
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
    {selected && <RunDetail id={selected} revision={revision} csrf={csrf} refresh={refresh} />}
  </div>
}
