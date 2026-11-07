{ ... }:
{
  imports = [
    ../exporters.nix
    ../gpu-telemetry.nix
  ];

  homelab.native.exporters.enable = true;
  homelab.native.applications.monitoring.enable = true;
}
