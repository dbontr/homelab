# Unified console capabilities

The first-party Rust/Dioxus console compiles to WebAssembly and is a same-origin client for Go-owned APIs. It does not depend on authored TypeScript or JavaScript application logic.
It does not receive the cluster credential, execute arbitrary MCP tools, or
treat local personal tasks as Workspace compute tasks.

## Owner workflow

- **Tasks:** Capture tasks with a limited deterministic natural-language
  vocabulary, mark them completed, repeat supported schedules and inspect an
  agenda. Update full task metadata using revision guards. A completion of a
  repeated task records a completed occurrence and creates a new
  due occurrence; it does not pretend an overdue task was completed.
- **Calendar:** Inspect advisory timebox conflicts across local task records
  and *event definitions* from a configured read-only iCalendar feed. Unsupported
  or unexpanded recurrences are reported as limitations. The system does not
  automatically modify external calendars.
- **Recipes:** Import a schema.org Recipe from pasted JSON-LD, HTML or a
  bounded public HTTPS page. The URL importer validates public DNS answers,
  pins TLS connections to resolved addresses, rejects redirects and proxies,
  and returns an unsaved draft. Missing ingredient quantities require review.
  Attach privately stored JPEG or PNG images from the browser editor; images
  are immutable, addressable by content hash and retrieved only with recipe
  read authorization. Review up to 20 earlier recipe versions and restore a
  retained snapshot only with the current revision, preserving the prior state.
  Review a RecipeSage JSON-LD export as a bounded batch, inspect per-recipe
  conversion warnings, correct incomplete entries in the same recipe editor,
  and select validated records for an atomic first-party import. The original RecipeSage database and export remain unchanged.
  Export recipes, scheduled meals and history together with referenced photos
  in a bounded, portable ZIP; import it without replacing existing records.
  Mark groceries as purchased
  through a revision-fenced persistent record.
- **Food and Health:** Nutrition associated with a first-party recipe may be
  submitted as an unconfirmed Health food proposal. An owner must separately
  confirm consumption before any observations are recorded. Manual Health
  observations declare whether each measurement was measured, reported or
  estimated. Strength, cardio and mobility entries may be linked to a plan
  and session; evidence-limited progression suggestions never alter plans.
- **Learning:** Begin a session for a preexisting Adaptive Teaching concept,
  view its phase and pending question, and record an ungraded learner response
  with explicit assistance disclosure. Only the education service can evaluate
  the response or award evidence-backed mastery.
- **Workspace:** Read project jobs and isolated workspace state without
  extending browser authorization to arbitrary code execution.

The console's look-and-feel continues to follow shadcn/ui's neutral/new-york
design vocabulary, but is implemented using Rust/Dioxus rather than React.

## Boundaries

This branch is a development implementation, not a feature-parity assertion.
It does not yet provide autonomous Codex-equivalent agents, full exercise
authoring and checking in the browser, richer Health coaching, robust recipe
imports from arbitrary PDFs or pages without structured Recipe markup, offline synchronization, scheduled
reminders, or calendar write-back. Those operations require their own
identity, authorization, evidence and data migration contracts.

No deployment, destructive migration or change to the Nix homepage cutover
flags is made by this branch.
