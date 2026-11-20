import { useEffect, useState } from "react"
import {
  Activity, BookOpen, Brain, CalendarDays, CircleUserRound, Cpu,
  FolderKanban, HeartPulse, LayoutDashboard, Library, ListTodo,
  MonitorCog, Moon, RefreshCcw, ScrollText, Server, Bell,
  ShieldCheck, Sun, Workflow, Wrench, Menu, X, Utensils,
  AlarmClock, ClipboardList, NotebookPen, type LucideIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Overview } from "@/views/Overview"
import { Projects } from "@/views/Projects"
import { Monitoring } from "@/views/Monitoring"
import { Notifications } from "@/views/Notifications"
import { Engineering } from "@/views/Engineering"
import { SoftwareLibrary } from "@/views/SoftwareLibrary"
import { Records, recordPages } from "@/views/Records"
import { useApi, type Session } from "@/lib/api"
import { cn } from "@/lib/utils"

type PageKey = "overview" | "projects" | "engineering" | "software" | "monitoring" | "notifications" | keyof typeof recordPages

type NavGroup = { group: string; items: Array<{ id: string; label: string; icon: LucideIcon }> }

const navigation: NavGroup[] = [
  { group: "Research", items: [
    { id: "overview", label: "Overview", icon: LayoutDashboard },
    { id: "projects", label: "Projects", icon: FolderKanban },
    { id: "engineering", label: "Engineering", icon: Wrench },
    { id: "compute", label: "Compute", icon: Cpu },
    { id: "profiles", label: "Build profiles", icon: ScrollText },
    { id: "workflows", label: "Workflows", icon: Workflow },
    { id: "software", label: "Software library", icon: Library },
  ] },
  { group: "Personal", items: [
    { id: "tasks", label: "Tasks", icon: ListTodo },
    { id: "calendar", label: "Calendar", icon: CalendarDays },
    { id: "planner", label: "Weekly planner", icon: ClipboardList },
    { id: "learning", label: "Learning", icon: Brain },
    { id: "health", label: "Health", icon: HeartPulse },
    { id: "recipes", label: "Recipes", icon: Utensils },
    { id: "pantry", label: "Pantry", icon: NotebookPen },
    { id: "books", label: "Books", icon: BookOpen },
    { id: "automation", label: "Automations", icon: AlarmClock },
  ] },
  { group: "System", items: [
    { id: "monitoring", label: "Monitoring", icon: MonitorCog },
    { id: "notifications", label: "Notifications", icon: Bell },
    { id: "activity", label: "Activity", icon: Activity },
    { id: "security", label: "Security", icon: ShieldCheck },
  ] },
]

const ids = new Set<string>(navigation.flatMap(group => group.items.map(item => item.id)))

function currentPage(): PageKey {
  const fragment = window.location.hash.slice(1)
  return ids.has(fragment) ? fragment as PageKey : "overview"
}

function ThemeButton({ dark, onClick }: { dark: boolean; onClick: () => void }) {
  return <Button size="icon" variant="outline" aria-label={dark ? "Use light appearance" : "Use dark appearance"} onClick={onClick}>
    {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
  </Button>
}

function SessionMessage({ reason }: { reason: string }) {
  return <div className="mx-auto flex min-h-screen max-w-xl items-center justify-center p-6">
    <Alert>
      <CircleUserRound aria-hidden="true" className="size-4" />
      <AlertTitle>Authenticated Homelab session required</AlertTitle>
      <AlertDescription>
        <p>{reason}</p>
        <p className="mt-2">Sign in through the existing Cloudflare Access application. The console cannot issue administrator credentials or bypass access control.</p>
      </AlertDescription>
    </Alert>
  </div>
}

export default function App() {
  const [page, setPage] = useState<PageKey>(currentPage)
  const [revision, setRevision] = useState(0)
  const [dark, setDark] = useState(() => window.matchMedia("(prefers-color-scheme: dark)").matches)
  const [mobileOpen, setMobileOpen] = useState(false)
  const session = useApi<Session>("/ui/session", revision)

  useEffect(() => {
    const update = () => { setPage(currentPage()); setMobileOpen(false) }
    window.addEventListener("hashchange", update)
    return () => window.removeEventListener("hashchange", update)
  }, [])

  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark)
  }, [dark])

  if (session.loading) {
    return <div role="status" className="mx-auto flex min-h-screen max-w-md items-center justify-center text-sm text-muted-foreground">Checking the protected browser session…</div>
  }
  if (session.error || !session.data?.authenticated) {
    return <SessionMessage reason={session.error ?? "The current browser session is not authenticated."} />
  }

  const title = navigation.flatMap(group => group.items).find(item => item.id === page)?.label ?? "Overview"
  const refresh = () => setRevision(v => v + 1)
  return <div className="flex min-h-screen bg-background text-foreground">
    <aside className={cn(
      "fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r bg-card transition-transform lg:sticky lg:top-0 lg:h-screen lg:translate-x-0",
      mobileOpen ? "translate-x-0" : "-translate-x-full",
    )}>
      <div className="flex h-16 items-center justify-between border-b px-5">
        <a href="#overview" className="font-semibold tracking-tight">Homelab</a>
        <Button className="lg:hidden" size="icon" variant="ghost" aria-label="Close navigation" onClick={() => setMobileOpen(false)}>
          <X className="size-4" />
        </Button>
      </div>
      <nav className="flex-1 space-y-6 overflow-y-auto px-3 py-5" aria-label="Main navigation">
        {navigation.map(group => <div key={group.group}>
          <p className="mb-2 px-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">{group.group}</p>
          <div className="space-y-0.5">{group.items.map(item => {
            const Icon = item.icon
            return <a key={item.id} href={`#${item.id}`} aria-current={page === item.id ? "page" : undefined}
              className={cn("flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors hover:bg-accent hover:text-accent-foreground",
                page === item.id && "bg-secondary font-medium")}>
              <Icon aria-hidden="true" className="size-4 shrink-0" />
              {item.label}
            </a>
          })}</div>
        </div>)}
      </nav>
      <div className="border-t px-5 py-3 text-xs text-muted-foreground">
        <div className="flex items-center gap-2"><Server className="size-3.5" aria-hidden="true" /> Go platform · authenticated</div>
      </div>
    </aside>
    {mobileOpen && <button type="button" className="fixed inset-0 z-30 bg-black/45 lg:hidden" aria-label="Close navigation backdrop" onClick={() => setMobileOpen(false)} />}
    <main className="min-w-0 flex-1">
      <header className="sticky top-0 z-20 flex h-16 items-center justify-between gap-3 border-b bg-background/95 px-5 backdrop-blur lg:px-8">
        <div className="flex items-center gap-3">
          <Button className="lg:hidden" size="icon" variant="outline" aria-label="Open navigation" onClick={() => setMobileOpen(true)}><Menu className="size-4" /></Button>
          <div>
            <h1 className="text-base font-semibold">{title}</h1>
            <p className="text-xs text-muted-foreground">Private computing platform</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="hidden sm:inline-flex">Protected session</Badge>
          <Button variant="outline" size="icon" aria-label="Refresh displayed records" onClick={refresh}>
            <RefreshCcw className="size-4" />
          </Button>
          <ThemeButton dark={dark} onClick={() => setDark(value => !value)} />
        </div>
      </header>
      <div className="mx-auto max-w-7xl space-y-6 p-5 lg:p-8">
        {page === "overview" ? <Overview revision={revision} /> :
          page === "projects" ? <Projects revision={revision} /> :
          page === "engineering" ? <Engineering revision={revision} csrf={session.data.csrf_token ?? ""} refresh={refresh} /> :
          page === "monitoring" ? <Monitoring revision={revision} /> :
          page === "notifications" ? <Notifications revision={revision} csrf={session.data.csrf_token ?? ""} /> :
          page === "software" ? <SoftwareLibrary revision={revision} /> :
          page in recordPages ? <Records definition={recordPages[page]} revision={revision} /> :
          <p className="text-muted-foreground">The selected view is unavailable.</p>}
      </div>
    </main>
  </div>
}
