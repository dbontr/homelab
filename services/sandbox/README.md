# Local engineering verification

The Rust executor accepts exactly `run WORKSPACE_ID GIT_SHA ACTION` and
restricts execution to a 32-character lowercase hexadecimal workspace ID,
an exact Git revision and one of six fixed build or test operations. The
operator configures `HOMELAB_SANDBOX_WORKSPACES` as an absolute directory
containing these workspaces. The executable must run as an unprivileged user.

The executor validates the Git checkout before use and runs the selected
tool in a Bubblewrap namespace with no network, read-only source and
`/nix/store`, private temporary storage, and no inherited environment or
host credentials. Inputs do not accept an executable name, arbitrary
arguments, project grants or remote deployment targets. A bounded timeout
and Bubblewrap's parent-death handling prevent detached build commands.

The Go engineering service retains source authorship, scheduling, project
authorization, revision receipts and publication control. This boundary
does not become a Kubernetes executor or authority to publish artifacts.
A source checkout and valid operator-configured workspace root are required.
Existing Kubernetes job isolation remains authoritative for remote builds.
