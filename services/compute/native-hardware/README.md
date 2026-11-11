# Native device validation

The worker's Go control plane remains responsible for leases, project
admission, execution backends and credential handling. The Rust helper
performs the live Linux device identity inspection used for approved USB
passthrough on the legacy host Docker backend.

`homelab-native-hardware map-usb ALIAS` accepts only a stable USB whole-disk
by-id alias, a stable USB serial by-id alias, or a three-digit USB bus path.
It resolves the alias, checks the character/block device kind and Linux
sysfs USB ancestry, then refuses block devices with mounted partitions,
non-disk children or uninspectable state. The result is the resolved device
path followed by its host group ID. The helper does not flash devices,
read their contents, grant a project lease or acquire host root authority.

A Docker compatibility worker selects the helper through the operator-set
`PWM_NATIVE_HARDWARE_HELPER` absolute executable path. Once configured,
native errors fail closed and cannot fall back to the Go validation code.
`PWM_NATIVE_USB_LSBLK` may select an operator-controlled absolute disk
inspection binary. The Nix package supplies util-linux by default.
Kubernetes device-plugin resources continue to use the Kubernetes
scheduler and do not map host USB block paths.

## Exclusive admission and recovery

The Docker compatibility worker can enable the Rust custody boundary with
`PWM_NATIVE_DEVICE_CUSTODY=1` after an operator selects an immutable absolute
`PWM_NATIVE_HARDWARE_HELPER`. The worker's systemd unit provisions the
private, persistent `/var/lib/homelab-device-leases` directory. The helper
enforces exact workspace IDs and creates exclusive, sync'd records for each
approved USB or accelerator alias before any container is created. Device
identity is checked against the record before and after Docker allocation.
Partial acquisitions can be rolled back by exact alias. After workspace stop,
the worker independently confirms the managed container is absent before
releasing its native records. If cleanup or identity checks are uncertain,
records remain reserved and recovery requires confirmed removal.

The helper does not issue controller leases, terminate containers, or accept
credentials. CDI and Kubernetes device-plugin resources continue under
their independently enrolled scheduler; they are not mapped through the raw
host-path helper. The host policy and worker authorization still distinguish
SSH inspection-only devices from enrolled compute devices.

Durable admission records serialize cooperating workers using the same
protected directory, not arbitrary root processes or tools that bypass it.
The container runtime still accepts device paths rather than inherited,
pinned file descriptors, so a sufficiently privileged host actor can replace
a device between an identity check and Docker's kernel mapping. Physical
exclusive custody therefore also requires OS-level device isolation and
hardware integration acceptance. Do not treat this source boundary alone
as verified physical exclusion.

## Accelerator character-device admission

The native helper also supports `map-char gpu|npu|fpga /dev/DEVICE`.
The operator supplies `HOMELAB_NATIVE_DEVICE_POLICY`, an immutable
Nix-store text file containing one exact `kind /dev/path` pair per line.
The helper verifies the approved path and current character device; it
does not issue leases or discover privileged device credentials.
The Go Docker compatibility worker delegates each raw accelerator mapping
to this native validator when `PWM_NATIVE_HARDWARE_HELPER` is configured.
Kubernetes extended resources continue using device-plugin scheduling.
