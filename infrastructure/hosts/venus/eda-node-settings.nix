{ ... }:
{
  homelab.edaBuildVm.enable = true;
  homelab.edaBuildVm.cpuCores = 3;
  homelab.edaBuildVm.memoryMiB = 8192;
  homelab.edaBuildVm.memoryOverheadMiB = 2048;
  homelab.edaBuildVm.hostSshPort = 22022;
  homelab.edaBuildVm.controlPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJfywbnsG5x1zDHz8nUxMtufe8Lhh0fctzvB9tM0Z5g2";
}
