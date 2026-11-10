{ pkgs }:
pkgs.rustPlatform.buildRustPackage {
  pname = "homelab-executor-authority";
  version = "0.1.0";
  src = ../services/executor;

  # Privileged code is not eligible for a Nix release without reviewed dependency resolution.
  cargoLock.lockFile = ../services/executor/Cargo.lock;

  cargoBuildFlags = [ "--bin" "executord" "--bin" "recovery" ];
  doCheck = true;
  cargoTestFlags = [ "--all-targets" ];
  postInstall = ''
    test -x "$out/bin/executord"
    test -x "$out/bin/recovery"
  '';

  meta = with pkgs.lib; {
    description = "Independent Rust privileged executor and NixOS recovery";
    platforms = platforms.linux;
    mainProgram = "executord";
  };
}
