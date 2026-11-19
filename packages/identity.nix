{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-identity";
  version = "0.1.0";
  src = ../services/core;
  vendorHash = null;
  subPackages = [ "cmd/homelab-identityd" ];
}
