{ ... }:
{
  homelab.edaBuildVm.enable = true;
  homelab.edaBuildVm.cpuCores = 4;
  homelab.edaBuildVm.memoryMiB = 10240;
  homelab.edaBuildVm.memoryOverheadMiB = 2048;
  homelab.edaBuildVm.hostSshPort = 22022;
  homelab.edaBuildVm.controlPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGDVR1bXiQaM4cEbXIujTqMCFCftkBp0RKp4Ziv5kXRc";
}
