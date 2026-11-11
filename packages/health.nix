{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-health";
  version = "0.1.0";
  src = ../services/health;
  vendorHash = pkgs.lib.fakeHash;
  subPackages = [ "cmd/personal-health-mcp" ];
}
