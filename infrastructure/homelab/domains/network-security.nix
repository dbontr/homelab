{ ... }:
{
  imports = [
    ../cloudflare.nix
    ../mcp-ingress.nix
    ../orchestration.nix
    ../recovery.nix
  ];

  homelab.native.applications.pihole.enable = true;
  homelab.native.kubernetes.enable = true;
}
