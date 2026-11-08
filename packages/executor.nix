{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-executor";
  version = "0.1.0";
  src = ../system;
  vendorHash = null;
  subPackages = [ "cmd/homelab-executord" "cmd/homelab-release" ];
}
