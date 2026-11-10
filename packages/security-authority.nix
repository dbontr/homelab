{ pkgs }:
pkgs.rustPlatform.buildRustPackage {
  pname = "homelab-security-authority";
  version = "0.1.0";
  src = ../services/security-authority;

  # Cargo.lock is maintained by the source-development toolchain. It is
  # required so the security release cannot select unreviewed dependencies.
  cargoLock.lockFile = ../services/security-authority/Cargo.lock;

  cargoBuildFlags = [
    "--bin" "securityd"
    "--bin" "security-preflight"
  ];

  doCheck = true;
  cargoTestFlags = [ "--all-targets" ];

  postInstall = ''
    test -x "$out/bin/securityd"
    test -x "$out/bin/security-preflight"
  '';

  meta = with pkgs.lib; {
    description = "Owner authentication and independent privileged authorization";
    platforms = platforms.linux;
    mainProgram = "securityd";
  };
}
