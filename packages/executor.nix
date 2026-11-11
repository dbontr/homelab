{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-executor";
  version = "0.1.0";
  src = ../services/core;
  vendorHash = null;
  subPackages = [ "cmd/homelab-executord" "cmd/homelab-release" "cmd/homelab-audit-recovery" ];
}
