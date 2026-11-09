{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-security";
  version = "0.1.0";
  src = ../services/core;
  vendorHash = null;
  subPackages = [ "cmd/homelab-securityd" "cmd/homelab-admin" ];
}
