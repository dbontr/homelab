import { useState, type FormEvent } from "react"
import { Camera, CodeXml, FileImage, Play } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { apiWrite, asRecord, list, text, useApi, validProject } from "@/lib/api"

const REPO = /^[A-Za-z0-9_.-]{1,100}\/[A-Za-z0-9_.-]{1,100}$/
const COMMIT = /^[0-9a-f]{40}$/
const SOURCE = /^[A-Za-z0-9._/-]{1,255}$/

function sourcePaths(input: string): string[] {
  return input.split("\n").map(value => value.trim()).filter(Boolean)
}

function safePaths(paths: string[], limit: number): boolean {
  return paths.length <= limit && new Set(paths).size === paths.length && paths.every(value =>
    SOURCE.test(value) && !value.startsWith("/") && !value.includes("//") &&
    value.split("/").every(part => part !== "." && part !== ".." && part !== ".git" &&
      part !== ".env" && !part.startsWith(".env.") && part !== ".ssh" &&
      part !== ".aws" && part !== "credentials.json" && part !== "secrets.json"))
}

export function VisualRunForm({ csrf, onSubmitted }: { csrf: string; onSubmitted: () => void }) {
  const catalog = useApi<unknown>("/v1/software")
  const images = list(asRecord(catalog.data)?.images).filter(image =>
    text(image, "status") === "active" && Array.isArray(image.available_nodes) &&
    image.available_nodes.length > 0)
  const [project, setProject] = useState("")
  const [repository, setRepository] = useState("")
  const [revision, setRevision] = useState("")
  const [baseBranch, setBaseBranch] = useState("main")
  const [image, setImage] = useState("")
  const [title, setTitle] = useState("Visual engineering review")
  const [objective, setObjective] = useState("")
  const [checks, setChecks] = useState("")
  const [allowedPaths, setAllowedPaths] = useState("")
  const [visualPaths, setVisualPaths] = useState("")
  const [outputPaths, setOutputPaths] = useState("")
  const [inputArtifacts, setInputArtifacts] = useState("")
  const [virtualDisplay, setVirtualDisplay] = useState(false)
  const [publishDraft, setPublishDraft] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [created, setCreated] = useState("")

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    setError("")
    setCreated("")
    const edit = sourcePaths(allowedPaths)
    const previews = sourcePaths(visualPaths)
    const deliver = sourcePaths(outputPaths)
    const importLines = sourcePaths(inputArtifacts)
    const imports = importLines.map(line => {
      const [artifact_id, ...pathParts] = line.split(/\s+/)
      return { artifact_id, path: pathParts.join(" ") }
    })
    const commands = sourcePaths(checks)
    if (!validProject(project) || !REPO.test(repository) || !COMMIT.test(revision) ||
        !images.some(item => text(item, "id") === image) ||
        !safePaths(edit, 16) || edit.length === 0 ||
        !safePaths(previews, 8) || previews.some(path => !path.toLowerCase().endsWith(".png")) ||
        !safePaths(deliver, 16) || imports.length > 8 ||
        imports.some(item => !/^[0-9a-f]{32}$/.test(item.artifact_id) || !safePaths([item.path], 1)) ||
        new Set(imports.map(item => item.path)).size !== imports.length ||
        (!virtualDisplay && previews.length === 0) ||
        commands.length === 0 || commands.length > 8 ||
        commands.some(cmd => cmd.length > 512 || /[\r\0]/.test(cmd)) ||
        objective.trim().length === 0 || objective.length > 4096 ||
        title.trim().length === 0 || title.length > 256 ||
        !/^[A-Za-z0-9][A-Za-z0-9._/-]{0,150}$/.test(baseBranch)) {
      setError("Specify a pinned repository, eligible image, approved edit paths, visual capture source and bounded verification commands.")
      return
    }
    if (!csrf) { setError("An authenticated browser session is required."); return }
    setBusy(true)
    try {
      const result = await apiWrite<unknown>("POST", "/v1/engineering/runs", csrf, {
        project, repository, revision, expected_head: revision, base_branch: baseBranch,
        image_pool_id: image, title, objective: objective.trim(),
        allowed_paths: edit, visual_paths: previews, output_paths: deliver, input_artifacts: imports,
        visual_feedback: true, virtual_display: virtualDisplay, autonomous: true,
        prepare_only: !publishDraft,
        cpu: 2, memory_mb: 4096, duration_minutes: 120, checks: commands,
      })
      const record = asRecord(result)
      setCreated(record ? text(record, "id", "run_id") || "Request accepted" : "Request accepted")
      onSubmitted()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Visual run submission failed")
    } finally {
      setBusy(false)
    }
  }

  return <Card>
    <CardHeader>
      <CardTitle className="flex items-center gap-2"><Camera className="size-5" aria-hidden="true" /> Visual engineering</CardTitle>
      <CardDescription>
        Use an approved compute workspace to render, capture, inspect and refine websites, circuits,
        photo edits or graphical applications. A vision-capable model and isolated execution image must be configured.
      </CardDescription>
    </CardHeader>
    <CardContent>
      <form onSubmit={submit} className="grid gap-4 md:grid-cols-2">
        <label className="space-y-1 text-sm">Project
          <Input required value={project} onChange={event => setProject(event.target.value)} placeholder="research-project" />
        </label>
        <label className="space-y-1 text-sm">Repository
          <Input required value={repository} onChange={event => setRepository(event.target.value)} placeholder="owner/repository" />
        </label>
        <label className="space-y-1 text-sm md:col-span-2">Exact source commit
          <Input className="font-mono text-xs" required value={revision} onChange={event => setRevision(event.target.value)} placeholder="40 lowercase hexadecimal characters" />
        </label>
        <label className="space-y-1 text-sm">Base branch
          <Input required value={baseBranch} onChange={event => setBaseBranch(event.target.value)} />
        </label>
        <label className="space-y-1 text-sm">Eligible environment
          <select value={image} onChange={event => setImage(event.target.value)} required
            className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm">
            <option value="">Choose an eligible image</option>
            {images.map(item => <option key={text(item, "id")} value={text(item, "id")}>{text(item, "id")}</option>)}
          </select>
          {images.length === 0 && <span className="block text-xs text-muted-foreground">No approved image has an eligible worker.</span>}
        </label>
        <label className="space-y-1 text-sm md:col-span-2">Run title
          <Input required value={title} onChange={event => setTitle(event.target.value)} />
        </label>
        <label className="space-y-1 text-sm md:col-span-2">Visual objective
          <Textarea required value={objective} onChange={event => setObjective(event.target.value)}
            placeholder="Inspect the rendered layout for crossings, overlaps, spacing, readability and visual consistency; improve the approved files until the result is acceptable." />
        </label>
        <label className="space-y-1 text-sm">Editable source paths (one per line)
          <Textarea required className="min-h-24 font-mono text-xs" value={allowedPaths}
            onChange={event => setAllowedPaths(event.target.value)}
            placeholder={"web/src/App.tsx\nweb/src/styles.css"} />
        </label>
        <label className="space-y-1 text-sm">PNG preview paths (one per line)
          <Textarea className="min-h-24 font-mono text-xs" value={visualPaths}
            onChange={event => setVisualPaths(event.target.value)}
            placeholder={"artifacts/desktop.png\nartifacts/mobile.png"} />
        </label>
        <label className="space-y-1 text-sm md:col-span-2">Approved build, render and test commands (one per line)
          <Textarea required className="min-h-24 font-mono text-xs" value={checks}
            onChange={event => setChecks(event.target.value)}
            placeholder={"npm run build\nnpm run test:visual"} />
        </label>
        <label className="space-y-1 text-sm md:col-span-2">Existing project input artifacts (optional, one artifact ID and repository-relative destination per line)
          <Textarea className="min-h-20 font-mono text-xs" value={inputArtifacts}
            onChange={event => setInputArtifacts(event.target.value)}
            placeholder="32-character-artifact-id photos/original.CR3" />
        </label>
        <label className="space-y-1 text-sm md:col-span-2">Deliverable paths (optional, one per line)
          <Textarea className="min-h-20 font-mono text-xs" value={outputPaths}
            onChange={event => setOutputPaths(event.target.value)}
            placeholder={"photos/finished.jpg\nphotos/finished.png\nphotos/original.CR3.xmp\nphotos/original.CR3"} />
        </label>
        <label className="flex items-center gap-2 text-sm md:col-span-2">
          <input type="checkbox" className="size-4" checked={virtualDisplay}
            onChange={event => setVirtualDisplay(event.target.checked)} />
          Capture the isolated graphical display (requires Xvfb, xdpyinfo and ffmpeg in the image)
        </label>
        <label className="flex items-center gap-2 text-sm md:col-span-2">
          <input type="checkbox" className="size-4" checked={publishDraft}
            onChange={event => setPublishDraft(event.target.checked)} />
          Prepare a revision-guarded draft pull request after verification
        </label>
        <p className="text-xs text-muted-foreground md:col-span-2">
          Image content is sent only to the explicitly configured vision model for this opted-in run.
          Only the owner-approved paths and commands can be changed or executed. Up to six screenshot
          observations are retained as project artifacts; an acceptable visual review is required after
          the last source edit.
        </p>
        <div className="md:col-span-2">
          <Button disabled={busy || images.length === 0 || !csrf} type="submit" className="w-full">
            <Play className="size-4" aria-hidden="true" /> {busy ? "Submitting…" : "Submit visual run"}
          </Button>
        </div>
        {error && <Alert variant="destructive" className="md:col-span-2">
          <AlertTitle>Visual run not submitted</AlertTitle><AlertDescription>{error}</AlertDescription>
        </Alert>}
        {created && <p className="flex items-center gap-2 text-sm md:col-span-2">
          <CodeXml className="size-4" aria-hidden="true" /> Request: <code className="break-all">{created}</code>
          <FileImage className="size-4" aria-hidden="true" />
        </p>}
      </form>
    </CardContent>
  </Card>
}
