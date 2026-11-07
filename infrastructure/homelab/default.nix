{ ... }:
{
  imports = [
    ./shared.nix
    ./domains/network-security.nix
    ./domains/diagnostics-observations.nix
    ./domains/downloads-media.nix
    ./domains/learn.nix
    ./domains/workspace-mcp.nix
    ./domains/personal-health.nix
  ];
}
