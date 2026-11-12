{ pkgs }:
let
  src = pkgs.fetchCrate {
    pname = "wasm-bindgen-cli";
    version = "0.2.130";
    hash = "sha256-9Og1O/6/UC3sc5hBjoIUBErnLW2UHGMsTQO+6+m/dng=";
  };
in
pkgs.rustPlatform.buildRustPackage {
  pname = "wasm-bindgen-cli";
  version = "0.2.130";
  inherit src;
  cargoHash = "sha256-j81sUf1m5yXU4FJfGj7GV+hdqUCV8mRmNb0QIsMCjqu=";
  doCheck = false;
}
