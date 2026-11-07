# Hardware and enrollment

Hardware identity and permission are separate. The inventory in `hardware.nix` declares both NixOS nodes and attached or remotely accessible devices. A source change does not alter a running machine.

## Enrolled compute nodes

An enrolled node is an explicitly managed NixOS host. Add an entry under `nodes` with a unique `id`, `mode = "enrolled"`, its `system` architecture and `role = "compute"`. Supply the compute node `nodeIP` and `lanInterface` network settings, and add the corresponding `infrastructure/hosts/<id>/configuration.nix` NixOS definition. Import the shared `infrastructure/modules/compute-worker.nix` worker template from the host module with its inventory-backed `hardwareNode`. The flake derives host outputs from enrolled entries; the compute worker template reads device-independent network and cluster settings from the same inventory. No separate machine-name list is maintained.

Remove the inventory entry to remove a host from the build graph. This does not decommission an existing host, delete disks, remove workloads or revoke credentials. Those operations require independent authorization.

## Agentless SSH devices

An SSH-mode device is not a managed compute worker. Add a `devices` entry:

```nix
{
  id = "lab-fpga";
  kind = "fpga";
  mode = "ssh";
  connection = "ssh";
  ssh = { host = "fpga.example"; user = "research"; port = 22; };
  allowedOperations = [ "connect" "inspect" ];
  eligibleForCompute = false;
}
```

The SSH target is optional so an unconfigured device can be inventoried without contacting it. No login is attempted merely by declaring one. Credentials, SSH agent state and trusted host keys belong outside Git. Use a non-root account and verify the remote host key independently.

Agentless access must not install workers, controllers or system services, modify system configuration, assume administrative permissions or obtain managed compute leases. A separately authorized SSH executor must enforce permitted operations and remote identity before any connection is enabled. The Go catalog validates inventory policy; it does not yet authorize remote execution.

## Device allocation

Managed GPU, NPU, FPGA and USB hardware must be associated with an enrolled worker. Its device permissions and `eligibleForCompute` status determine whether it can be considered for allocation. The executor must bind the declared ID to the physical device, validate permission at execution time and fail closed when the inventory or identity changes.

Permissions never depend on manufacturer, model or ownership. Enrollment changes require the same independent acceptance checks as other infrastructure changes.
