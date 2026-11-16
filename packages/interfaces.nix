{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-interfaces";
  version = "0.1.0";
  src = ../.;
  env.GOWORK = "off";
  vendorHash = "sha256-zyZC+MXpsRrcbIa03yYQfWtihmF1Ja2kb6LEObEtcGo=";
  subPackages = [ "mcp" "api" ];
  postInstall = ''
    mv "$out/bin/mcp" "$out/bin/homelab-mcp"
    mv "$out/bin/api" "$out/bin/homelab-api"
  '';
}
