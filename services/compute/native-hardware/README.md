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

The Rust helper performs validation at invocation, not exclusive physical
custody of the device. Host device replacement between validation and
Docker mapping remains a separate hardware execution limitation. Enabling
USB passthrough requires explicit operator permission; the helper does not
imply authority for SSH inspection-only devices.

## Accelerator character-device admission

The native helper also supports `map-char gpu|npu|fpga /dev/DEVICE`.
The operator supplies `HOMELAB_NATIVE_DEVICE_POLICY`, an immutable
Nix-store text file containing one exact `kind /dev/path` pair per line.
The helper verifies the approved path and current character device; it
does not issue leases or discover privileged device credentials.
The Go Docker compatibility worker delegates each raw accelerator mapping
to this native validator when `PWM_NATIVE_HARDWARE_HELPER` is configured.
Kubernetes extended resources continue using device-plugin scheduling.
