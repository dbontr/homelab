{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-calendar";
  version = "0.1.0";
  src = ../services/core;
  vendorHash = null;
  subPackages = [ "cmd/homelab-calendard" ];
}
