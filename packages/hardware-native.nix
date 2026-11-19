{ pkgs }:
pkgs.buildGoModule {
  pname = "homelab-native-hardware";
  version = "0.1.0";
  src = ../services/native-hardware;
  vendorHash = null;
  subPackages = [ "." ];
  doCheck = true;
  ldflags = [
    "-X main.dockerBinary=${pkgs.docker}/bin/docker"
    "-X main.lsblkBinary=${pkgs.util-linux}/bin/lsblk"
  ];
  postInstall = ''
    mv "$out/bin/native-hardware" "$out/bin/homelab-native-hardware"
    test -x "$out/bin/homelab-native-hardware"
  '';

  meta = with pkgs.lib; {
    description = "Native device identity and exclusive custody validator";
    platforms = platforms.linux;
    mainProgram = "homelab-native-hardware";
  };
}
