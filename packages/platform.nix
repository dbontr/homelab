{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-platform";
  version = "0.1.0";
  src = ../system;
  vendorHash = null;
  subPackages = [ "cmd/homelab-platform" "cmd/homelab-approve" ];
}
