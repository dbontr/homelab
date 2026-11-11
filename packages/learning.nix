{ pkgs }:
(pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
  pname = "homelab-learning";
  version = "0.1.0";
  src = ../services/learning;
  vendorHash = "sha256-NbuubsnNbz3aKajjyfpkbi52qRhX1473j3LuaUDn3L8=";
  subPackages = [ "cmd/adaptive-teaching" ];
}
