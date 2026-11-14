{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-health";
  version = "0.1.0";
  src = ../services/health;
  vendorHash = "sha256-dsmRXd5moOA08U2Hbi9Z3Hy1inZFiDOD9AMS56uk+8g=";
  subPackages = [ "cmd/personal-health-mcp" ];
}
