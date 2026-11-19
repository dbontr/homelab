# Native device custody

The Go helper enforces Linux device identity and exclusive custody for the
Compute Docker compatibility backend. The controller owns project admission,
hardware leases and workspace lifecycle. This process does not create a
controller lease or destroy a container.

The immutable NixOS package provides `homelab-native-hardware`. The
`map-usb` command accepts a stable USB whole-disk or serial by-id alias,
or a three-digit `/dev/bus/usb` address. Each device is checked against
its current Linux node type and sysfs USB ancestry. A block device is denied
when it has active mapped children or mounted volumes. A bus address is
inspection-only and cannot establish durable custody.

The `map-char` command accepts only `gpu`, `npu` or `fpga`, an exact
`/dev` character device, and a match in the operator's immutable
`HOMELAB_NATIVE_DEVICE_POLICY` Nix-store file.

The `custody-v1` protocol uses private, persistent, version-two records
in `/var/lib/homelab-device-leases` or the configured protected runtime
directory. Both the original alias and the kernel physical device number
must be reserved. The records also bind the current filesystem device
number and inode. Records are created exclusively and synced to disk.
An interrupted claim keeps its physical reservation; a stale or malformed
record denies use until a controlled recovery.

`acquire-device` and `verify-device` compare the current kernel device
identity to both records. `verify-workspace` checks all active device
records for an exact workspace ID. `release-device` and
`release-workspace` first use an immutable Docker executable to verify
that the exact managed container is absent. The helper removes alias
records before physical reservations.

Docker and lsblk executable paths are fixed in the Nix package at link
time; callers cannot substitute executables through environment variables.
The state directory must be private and owned by the executing service.
All commands reject unknown device classes, invalid workspace IDs and
unexpected state files. Container and device allocation remain restricted
by the worker's independent policy and operator approval.

**Limit:** Docker accepts host device paths rather than pinned open file
descriptors. An independently privileged host process could replace a node
between verification and kernel mapping. Physical isolation and acceptance
tests on the enrolled hardware are therefore necessary before treating raw
passthrough as exclusive.
