# Visual inspection and media deliverables

Engineering visual inspection is an opt-in capability of an approved,
project-scoped compute workspace. It supports browser screenshots, virtual
desktop captures, exported schematic previews, and nondestructive photo
processing without granting access to a user's physical desktop.

## Execution contract

An autonomous Engineering program enables `visual_feedback=true` and
supplies the exact Git revision, project, approved compute image, verification
commands and allowed source files. It must additionally select at least one
capture source:

- `visual_paths`: up to eight repository-relative PNG paths created by the
  project's approved build or render commands. The agent may inspect only
  these exact paths.
- `virtual_display=true`: requests an isolated Xvfb display in the
  workspace. The worker captures that display through ffmpeg. This requires
  the approved image to contain Xvfb, xdpyinfo and ffmpeg.

The agent selects the `observe` action after an approved render command.
Compute uploads the PNG into the project's immutable artifact store.
Engineering requests it through the project-authorized MCP
`artifact_view_image` endpoint and verifies its project identity, SHA-256,
PNG structure, size and dimensions before sending it to the configured
vision-capable model.

A single review returns `acceptable` or `needs_revision`, up to eight
specific defects, and up to eight suggested changes. The authoring model
receives the bounded review observation and may revise only `allowed_paths`
through the existing revision-checked editing operations. Source changes
invalidate prior visual acceptance. A successful autonomous run requires
all approved verification commands and a satisfactory subsequent review.
Each run permits at most six captured reviews. This is a model judgment
about visible quality, not a mathematical quality guarantee.

The owner authorizes the transfer of screenshots to the selected model by
enabling visual feedback. No model sees the host screen or unselected files,
and screenshot bytes are not saved in Engineering transcripts. Model
instructions are not accepted from image content. Screenshot evidence,
visual comments, source revisions and deliverable artifact IDs remain
available for subsequent owner inspection.

## Websites and circuit diagrams

The approved render command can use Playwright or Chromium inside the
workspace to produce PNGs at known viewport sizes. For Playwright, use the
supported `page.screenshot({path, fullPage:true})` operation, with paths
under the exact repository checkout. Capture representative desktop and
mobile layouts, then inspect overlaps, clipping, typography and spacing.
Browser egress and served origins are controlled by the workspace's
project policy; screenshot capture does not grant host browser access.

For LTspice or other graphical electrical tools, the same `observe` action
can inspect the application's isolated virtual display if a supported
execution adapter and licensed runtime are independently available.
Otherwise, generate a PNG preview using an approved schematic renderer.
An exported netlist, simulated waveform and visual screenshot serve
different purposes: an aesthetically tidy schematic does not prove that
its circuit is electrically correct. Always retain simulation or
compiler checks separately.

## Darktable and nondestructive RAW editing

Darktable accepts CR3 files subject to camera-specific decoding support.
The original RAW file must remain immutable. Darktable stores most edits in
an XMP sidecar; some configurations also create DTDATA sidecars.

A project may import existing immutable artifacts into a workspace by
`input_artifacts`, each with an existing project artifact ID and an exact
repository-relative destination. Each destination must be absent and the
imported bytes must match the controller's recorded SHA-256 before use.
A RAW, sidecar, LUT, style or reference image is not assumed to exist
merely because its filename is declared.

A checked-in, owner-approved render script can call:

```sh
darktable-cli photos/original.CR3 photos/original.CR3.xmp photos/review.png
darktable-cli photos/original.CR3 photos/original.CR3.xmp photos/final.jpg
```

The exact flags, imported sidecar and output format must match the
installed Darktable version. Include a generated PNG in `visual_paths`
for inspection. The agent can refine an approved script or sidecar,
rerender, inspect and repeat within its visual-observation budget.
A normal darktable-cli export does not by itself produce a reusable
editing sidecar; preserve the actual XMP created by the approved editing
workflow.

The `output_paths` list selects up to sixteen exact repository-relative
files to store as immutable project artifacts after successful verification.
Typical entries include `photos/final.jpg`, `photos/final.png`,
`photos/original.CR3.xmp` and `photos/original.CR3`. The controller
checks each file's exported SHA-256 against its workspace digest, and
records its project-scoped artifact ID and size. JPEG/PNG previews are
not substitutes for the RAW original and edit history.

For larger photos or RAW files, use the existing authenticated binary
artifact upload routes. The native Engineering console also provides a
project-scoped browser download endpoint for confirmed artifacts up to
256 MiB. It retrieves the entire binary from the configured protected
Compute endpoint, verifies its SHA-256 before returning any bytes, and
provides a private attachment response without giving the browser an
upstream controller credential. Larger files use the Compute controller's
existing authenticated artifact download service. Small MCP inline
attachments have much lower size limits. Storage limits and backups remain governed by
Compute. A file cannot be shared through a project that does not
authorize the caller.

## Program status and operations

Catalog entries for Darktable, darktable-cli and Playwright are requests
until the operator reviews licenses, installs exact-version dependencies,
approves the image and qualifies it on a worker. An approved container
image does not imply an available graphical session, a browser runtime, a
GPU allocation or a Windows/macOS native application adapter.

Engineering still requires an independently verified non-administrator
project editor identity and a protected Go platform release. No screenshot
feature changes security authority, owner sessions, worker image admission,
network policy or the protected release boundary.
