{ pkgs }:
let
  src = pkgs.fetchCrate {
    pname = "wasm-bindgen-cli";
    version = "0.2.130";
    hash = "sha256-9Ug1O/6/UC3sc5hBj01UBErnLW2UHGMsTQ0+6+m/dng=";
  };
in
pkgs.rustPlatform.buildRustPackage {
  pname = "wasm-bindgen-cli";
  version = "0.2.130";
  inherit src;
  cargoLock.lockFile = "${src}/Cargo.lock";
  doCheck = false;
}
