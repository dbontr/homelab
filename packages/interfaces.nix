{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-interfaces";
  version = "0.1.0";
  src = ../.;
  GOWORK = "off";
  vendorHash = null;
  subPackages = [ "mcp" "api" ];
  postInstall = ''
    mv "$out/bin/mcp" "$out/bin/homelab-mcp"
    mv "$out/bin/api" "$out/bin/homelab-api"
  '';
}
