{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-engineering";
  version = "0.1.0";
  src = ../system;
  vendorHash = null;
  subPackages = [ "cmd/homelab-engineeringd" ];
}
