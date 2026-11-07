# Compute worker enrollment

The cluster agent module defaults to Saturn's K3s control plane at
`10.77.30.1:6443` on the compute VLAN. It binds Flannel to the verified
`lanInterface` and rejects a worker with Saturn's hostname or control-plane IP. Each machine must have its actual
operating system, storage, data protection, network interface and address
verified before enabling the module. Do not overwrite or reinstall an existing
system to enroll a worker.

For a verified NixOS worker, import `nixosModules.clusterAgent` and set
`homelab.clusterAgent.enable = true`, `nodeIP`, and `lanInterface` to the
observed machine values. Confirm the configured `controlPlane` remains
`10.77.30.1`. Preserve existing host-specific NixOS modules and storage
configuration.

Provision the join credential out of band at the module's `tokenFile` path
(default `/var/lib/secrets/k3s-agent-token`) with root-only access. Do not
place the token in Git, the Nix store, command arguments, shell history,
unencrypted logs, or a Kubernetes ConfigMap.

Before requesting approval, confirm the node's hardware identity and that
the K3s API is reachable over the authorized compute VLAN. Verify its stable
node IP, Ready condition, Flannel connectivity, expected node labels and
absence of unexpected control-plane roles. Readiness alone does not prove
machine identity. After this verification, run on Saturn:

    /etc/nixos/homelab/approve-worker.sh NODE

The approval adds the NodeRestriction-protected compute-worker label.
Workers cannot self-assign it. Test a restricted, non-privileged Pod, DNS,
inter-node network connectivity and a reboot/rejoin. A worker's loss must not
interrupt Saturn's routing or native critical applications.

The worker-agent NixOS module must install a `registries.yaml` mirror entry for
`images.dbontr.internal` on every participating node. K3s' embedded registry
must be enabled on the controller. Its peer-to-peer discovery uses TCP 5001,
and workers must accept the private image-sharing service on TCP 6443. A mirror
entry on Saturn alone does not enable distribution on the agents. Verify a
cold image pull on a worker before updating the gateway Deployment image.

Workers must reach the control-plane API on TCP 6443 and peers on the
required K3s ports, including Flannel VXLAN UDP 8472, kubelet TCP 10250
and the embedded registry TCP 5001. The embedded OCI mirror uses the
virtual registry name `images.dbontr.internal`, which does not need an
external DNS record.

Repeat enrollment independently for Jupiter, Venus, Mercury and Janus
only after each host's OS, storage, connectivity and backups are verified.
Do not import `tank_gitlab` or migrate Saturn-resident applications as part
of worker enrollment.

## NanoKVM console sequence

Keep Saturn powered on and its network cabling unchanged when moving the
single NanoKVM to a worker. Connect video and USB control to **one** worker
at a time; keep NanoKVM's own network and power connection intact.

1. Inspect the installed operating system, actual boot/root filesystems,
   storage devices and existing data before editing boot or disk settings.
   On Linux, run `sh homelab/worker-inventory.sh` if the repository is
   available, or use the equivalent read-only console commands. The script
   does not inspect secret files or change settings.
2. Confirm the machine's hostname, network connection, VLAN assignment,
   CPU, memory, GPU and the machine's existing SSH identity. Identify the
   actual switch port and whether the port is a tagged trunk or untagged
   VLAN 30 access port. Do not assume the interface name or worker IP.
3. For a bootable NixOS installation, inspect its own NixOS configuration
   and hardware module. Preserve its filesystem UUIDs, user accounts,
   home directories and installed data. Make only host-specific corrections
   supported by the live inventory; validate with `nixos-rebuild build`
   before applying changes. Keep NanoKVM attached through reboot validation.
4. Establish stable, non-overlapping addressing on the compute VLAN,
   confirm reachability to Saturn at `10.77.30.1:6443`, and establish
   authenticated, host-key-verified management access from Saturn where
   available. Do not enable password SSH for convenience.
5. Configure the existing cluster-agent module with the verified
   `nodeIP` and `lanInterface`. Provision a distinct, temporary worker
   enrollment credential if practical, through a secure channel. The
   central credential must never appear in the repository, chat, or logs.
   Do not enable K3s if the host still has a conflicting router/server
   identity or unresolved network and storage problems.
6. With the agent joined, inspect the node on Saturn and independently
   verify its physical identity. Only then run `approve-worker.sh`.
   Confirm the worker-only admission controls, a nonprivileged Pod, cluster
   DNS, Flannel connectivity and reboot/rejoin behavior. Retain a
   tested rollback path until the node is stable.
7. Repeat in order: Jupiter, Venus, Mercury, Janus. Do not assume that
   the four machines run identical operating systems or have matching
   storage layouts. A machine is not enrolled until it is Ready and its
   identity and workload acceptance have been verified.

### Jupiter's transplanted system SSD

Jupiter's NixOS SSD was originally installed in Saturn **before** Saturn's
current router and application configuration was created. This does not
establish that Jupiter is a current copy of Saturn. Inspect the actual
configuration rather than presuming duplicate router services.

Check the current filesystem mounts against the disk UUIDs in
`/etc/nixos/hardware-configuration.nix`, and inspect firmware/boot entries,
kernel modules, GPU PCI detection, and the live network interface names.
Use `nixos-generate-config --show-hardware-config` only as an inspection
aid; do not automatically replace hardware or boot configuration.
Preserve the existing bootable generation, files and user identities.
Confirm the detected NVIDIA model and installed driver before selecting
a driver or enabling GPU container workloads. Correct the host identity
and graphics configuration in place, rebuild, then validate a controlled
reboot before K3s enrollment.

### Control-plane credential handoff

Saturn has a K3s agent enrollment token at a root-protected server path.
Do not print, paste, log, or commit the token. Moving NanoKVM removes the
current direct-console execution path to Saturn. A validated secure
management connection or a later return to Saturn's console is necessary
before transferring any credential to a candidate worker. Never open
Saturn's SSH or Kubernetes administration ports to an untrusted network
merely to avoid a second console visit.

A protected worker label authorizes placement only within the
restricted compute namespaces. It does not prohibit trusted application,
GPU and batch workloads in other explicitly configured namespaces from
using the worker's CPU, memory and GPU. Saturn's existing critical
applications remain independent of the workers.
