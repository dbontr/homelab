{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-engineering-sandbox";
  version = "0.1.0";
  src = ../services/sandbox;
  vendorHash = null;
  doCheck = true;
  subPackages = [ "." ];
  ldflags = [
    "-X main.gitBinary=${pkgs.git}/bin/git"
    "-X main.bubblewrapBinary=${pkgs.bubblewrap}/bin/bwrap"
    "-X main.goBinary=${pkgs.go}/bin/go"
    "-X main.nixBinary=${pkgs.nix}/bin/nix"
  ];
  postInstall = ''
    mv "$out/bin/sandbox" "$out/bin/homelab-engineering-sandbox"
    test -x "$out/bin/homelab-engineering-sandbox"
  '';
  meta = with pkgs.lib; {
    description = "Revision-bound and network-isolated Go engineering verifier";
    platforms = platforms.linux;
    mainProgram = "homelab-engineering-sandbox";
  };
}
