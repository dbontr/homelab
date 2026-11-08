{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-fleet";
  version = "0.1.0";
  src = ../system;
  vendorHash = null;
  subPackages = [ "cmd/homelab-fleetd" ];
}
