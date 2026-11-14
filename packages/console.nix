{ pkgs }:
let
  wasmBindgen = import ./wasm-bindgen-cli.nix { inherit pkgs; };
in
pkgs.rustPlatform.buildRustPackage {
  pname = "homelab-console";
  version = "0.1.0";

  src = ../ui/console;
  cargoLock.lockFile = ../ui/console/Cargo.lock;

  nativeBuildInputs = with pkgs; [
    dioxus-cli
    binaryen
    lld
    pkg-config
    stdenv.cc
    wasmBindgen
  ];

  doCheck = false;
  dontStrip = true;

  buildPhase = ''
    runHook preBuild
    dx bundle --web --release --out-dir "$TMPDIR/bundle"
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall
    mkdir -p "$out"
    cp -a "$TMPDIR/bundle/public/." "$out/"
    test -f "$out/index.html"
    test -n "$(find "$out" -name '*.wasm' -type f -print -quit)"
    runHook postInstall
  '';
}
