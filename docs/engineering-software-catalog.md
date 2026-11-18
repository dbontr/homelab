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
