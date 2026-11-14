{ pkgs }:
pkgs.stdenv.mkDerivation {
  pname = "homelab-native-hardware";
  version = "0.1.0";
  src = ../services/compute/native-hardware;
  nativeBuildInputs = [ pkgs.rustc ];
  # The native boundary never resolves a caller-supplied Docker or disk
  # inspector executable. The immutable Nix store paths are compiled in.
  buildPhase = ''
    runHook preBuild
    export HOMELAB_NATIVE_DOCKER_BIN="${pkgs.docker}/bin/docker"
    export HOMELAB_NATIVE_LSBLK_BIN="${pkgs.util-linux}/bin/lsblk"
    rustc --edition=2021 -D warnings -C opt-level=2 src/main.rs -o homelab-native-hardware
    runHook postBuild
  '';
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    export HOMELAB_NATIVE_DOCKER_BIN="${pkgs.docker}/bin/docker"
    export HOMELAB_NATIVE_LSBLK_BIN="${pkgs.util-linux}/bin/lsblk"
    rustc --edition=2021 -D warnings --test src/main.rs -o native-hardware-tests
    ./native-hardware-tests
    runHook postCheck
  '';
  installPhase = ''
    runHook preInstall
    install -Dm755 homelab-native-hardware "$out/bin/homelab-native-hardware"
    runHook postInstall
  '';
  meta = with pkgs.lib; {
    description = "Native device identity validation for scoped compute-worker leases";
    platforms = platforms.linux;
    mainProgram = "homelab-native-hardware";
  };
}
