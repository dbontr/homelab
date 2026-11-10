{ pkgs }:
pkgs.rustPlatform.buildRustPackage {
  pname = "homelab-motivation";
  version = "0.1.0";
  src = ../services/motivation;
  cargoLock.lockFile = ../services/motivation/Cargo.lock;
  doCheck = true;
  cargoTestFlags = [ "--all-targets" ];

  postInstall = ''
    test -x "$out/bin/homelab-motivation"
  '';

  meta = with pkgs.lib; {
    description = "Private owner-directed identity and motivation service";
    platforms = platforms.linux;
    mainProgram = "homelab-motivation";
  };
}
