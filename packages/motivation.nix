{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-motivation";
  version = "0.1.0";
  src = ../services/motivation;
  vendorHash = null;
  subPackages = [ "cmd/homelab-motivation" ];
  doCheck = true;

  postInstall = ''
    test -x "$out/bin/homelab-motivation"
  '';

  meta = with pkgs.lib; {
    description = "Private owner-directed identity and motivation service";
    platforms = platforms.linux;
    mainProgram = "homelab-motivation";
  };
}
