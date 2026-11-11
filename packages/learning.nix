{ pkgs }:
(pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
  pname = "homelab-learning";
  version = "0.1.0";
  src = ../services/learning;
  vendorHash = "sha256-NbuvbsmNbz3aKdjyyfpkbi52yRhXl473j3LvAUDn3L8=";
  buildInputs = [ pkgs.sqlite.dev ];
  nativeBuildInputs = [ pkgs.pkg-config ];
  subPackages = [ "cmd/adaptive-teaching" ];
}
