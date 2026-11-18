# Engineering software catalog

Homelab Engineering uses the Compute controller as the sole authority for program
requests, approved OCI images, software packages, execution environments and
worker eligibility. Programs are independent of projects. Each project selects
an approved versioned environment when creating a workspace or submitting a
build.

## Program requests

The source catalog is at
`services/compute/catalog/engineering-programs.json`.
Each record has a stable ID, publisher name and source URL, version selection,
execution platform, distribution method, description and license notes.

From `services/compute`, validate the complete manifest without a
controller connection:

```sh
go run ./cmd/engineering-catalog
```

An independently authorized controller operator can register missing
requests without changing existing approval states:

```sh
go run ./cmd/engineering-catalog \
  -apply \
  -controller-url https://controller.example.invalid \
  -token-file /path/to/protected/controller-owner.token
```

The apply command requires an authenticated controller owner API, a private
regular credential file, an HTTPS endpoint (or a local loopback listener), and
a valid source manifest. It reads existing program identities before writing.
Matching IDs are preserved, including their revision and review state.
Conflicting identities stop the import. Interrupted imports can be repeated;
the importer does not replace or activate existing records. Do not use an
Engineering service-editor credential for operator catalog administration.

A request is not an installation:

- `requested` records the publisher and intended platform.
- `reviewed` means an operator inspected the software and its terms.
- `available` requires an independently approved active image or package
  collection. It does not assert that any worker can execute the software.
- `disabled`, `rejected` and `retired` prevent use or indicate the operator's
  decision without deleting historical records.

## Runtime qualification

For Linux compilers and command-line tools, use version-pinned OCI images or
approved, reproducible package collections. The image catalog and every
worker's advertised images must both authorize the selected reference. Confirm
the actual image digest, installed executable versions, CPU and memory limits,
network and filesystem isolation, then run a real compile or simulation. Keep
source revisions, commands, exit codes and results in the project's durable
records.

For interactive Linux applications such as Vivado, KiCad or FreeCAD, separately
qualify a protected graphical session; listing an OCI program does not imply
browser-based access to its graphical interface. Proprietary EDA installations
must use vendor-authorized downloads, verified checksums, accepted terms and
locally protected binaries and licenses. Never store proprietary installers,
licensed binaries, entitlement keys or user sign-in credentials in the public
repository or public container registry. FPGA programming, JTAG, USB devices,
GPUs and other hardware need independent leases and operator controls.

LTspice is cataloged with native Windows and macOS variants. No Linux worker
may claim native LTspice availability without a separately approved platform
and adapter. Use ngspice for native Linux batch SPICE workflows while native
graphical execution is unavailable.

## Publisher account and licensed installer access

The application catalog includes MATLAB R2026b and a Minecraft: Java Edition
reference installation. These are **requests**, not available Compute programs.
Neither record authorizes installation, logs into a publisher, or makes
licensed binaries available to a project. MATLAB needs a MathWorks account
for the installer and an appropriate license and toolbox entitlements for
execution. The Minecraft entry is for an owner-licensed reference game;
research projects may implement their own mechanics but must not copy or
redistribute proprietary game code, assets, or account material.

There are two distinct credential uses:

1. **Acquisition:** the owner authenticates directly with MathWorks, AMD,
   Microsoft, or another publisher to access the official installer. Browser
   login is interactive and remains in an operator-controlled session.
2. **Runtime:** a licensed application may need a separate activation, license
   server, or approved account session. Only a dedicated, least-privileged
   runtime identity or explicitly approved lease may be made available to
   that application.

Homelab already owns an encrypted credential vault in the independent security
authority. Its owner-only CLI supports `homelab-admin credential-add PROVIDER`,
`credential-list`, `credential-rotate`, and `credential-delete`. The owner
enters credential material on a trusted terminal using a private prompt;
ChatGPT, the general MCP gateway, source control, CI and workers must never
receive account passwords, browser cookies, refresh tokens or installer keys.
Providers with a documented, compatible OAuth authorization-code or device
flow may be enrolled through the existing protected OAuth broker after the
operator configures exact official endpoints, client identity and scopes.

**A generic credential record is not a working vendor login adapter.**
MathWorks, AMD and Microsoft's interactive sign-in and licensing workflows
must each undergo provider-specific policy review; do not guess OAuth support,
automate passwords against undocumented endpoints, export browser sessions, or
redeem protected credentials in an untrusted worker. Until a reviewed adapter
exists, acquisition uses a private owner-controlled browser and a verified
import of downloaded files. Never invoke a service-credential ticket merely
to send a secret to a caller-selected URL.

An installer must be accepted independently of account authentication:

- Read the official publisher's download page and terms from the catalog.
- The owner signs in directly to that publisher and selects the entitled
  product, release and target operating system.
- Acquire the installer into private operator-controlled storage; verify its
  source and SHA-256, preserve a private acquisition receipt, and scan the
  artifact before installation.
- Require a separately approved installation or runtime adapter with the
  correct software license, network constraints, Compute image digest, worker
  eligibility and project/hardware permissions.
- Keep acquisition credentials and licensing state outside the OCI layer,
  BuildKit cache, workspace files, command line, Project artifacts and logs.
  Revoke temporary access after use.

This distinction applies equally to publisher portals used only to obtain
installers and to applications that authenticate when launched. See
`docs/privileged-sessions.md` and `docs/security.md` for the existing
encrypted-vault, OAuth and exact-operation credential-grant contracts.

## Console behavior

The React Software Library uses the existing authenticated `/v1/software`
response from the Compute software catalog. It displays program versions,
review status, publisher links, approved image pools, package collections and
online worker eligibility. The Engineering verification form selects only
active image-pool IDs with eligible workers; it does not infer runtime
availability from a program request.

No catalog action merges a source branch, replaces the production security
authority, changes networking, installs software on a host, or grants access
to privileged hardware. Those remain independent protected operations.
