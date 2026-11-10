{ pkgs }:
assert builtins.pathExists ../services/security-authority/Cargo.lock;
pkgs.rustPlatform.buildRustPackage {
  pname = "homelab-security-authority";
  version = "0.1.0";
  src = ../services/security-authority;
  cargoLock.lockFile = ../services/security-authority/Cargo.lock;
  doCheck = true;
  cargoTestFlags = [ "--all-targets" ];
}
