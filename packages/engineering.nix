{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-engineering";
  version = "0.1.0";
  src = ../services/core;
  vendorHash = null;
  subPackages = [ "cmd/homelab-engineeringd" ];
}
