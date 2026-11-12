{ pkgs }:
let
  src = pkgs.fetchCrate {
    pname = "wasm-bindgen-cli";
    version = "0.2.130";
    hash = "sha256-9Og1O/6/VC3sc5hBjoIWBErmLW2UHGMsTQO+6+m/dng=";
  };
in
pkgs.rustPlatform.buildRustPackage {
  pname = "wasm-bindgen-cli";
  version = "0.2.130";
  inherit src;
  cargoHash = "sha256-j81sUf1m5yXU4FJfGj7GV+hdqVCV8mRmNb0QIsMCjqw=";
  doCheck = false;
}
