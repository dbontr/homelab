{ pkgs }:
pkgs.buildGo127Module {
  pname = "homelab-compute-console";
  version = "0.1.0";
  src = ../services/compute;
  vendorHash = null;
  subPackages = [ "cmd/project-workspace-console" ];
  nativeBuildInputs = [ pkgs.pkg-config ];
  buildInputs = [ pkgs.sqlite ];
  env.CGO_ENABLED = "1";
}
