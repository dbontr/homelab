{ pkgs }:
(pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
  pname = "homelab-learning";
  version = "0.1.0";
  src = ../services/learning;
  vendorHash = pkgs.lib.fakeHash;
  subPackages = [ "cmd/adaptive-teaching" ];
}
