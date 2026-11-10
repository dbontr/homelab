{ pkgs }:
pkgs.stdenv.mkDerivation {
  pname = "homelab-native-hardware";
  version = "0.1.0";
  src = ../services/compute/native-hardware;
  nativeBuildInputs = [ pkgs.rustc pkgs.makeWrapper ];
  buildPhase = ''
    runHook preBuild
    rustc --edition=2021 -D warnings -C opt-level=2 src/main.rs -o homelab-native-hardware
    runHook postBuild
  '';
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    rustc --edition=2021 -D warnings --test src/main.rs -o native-hardware-tests
    ./native-hardware-tests
    runHook postCheck
  '';
  installPhase = ''
    runHook preInstall
    install -Dm755 homelab-native-hardware "$out/bin/homelab-native-hardware"
    wrapProgram "$out/bin/homelab-native-hardware" --set-default HOMELAB_NATIVE_LSBLK "${pkgs.util-linux}/bin/lsblk"
    runHook postInstall
  '';
  meta = with pkgs.lib; {
    description = "Native device identity validation for scoped compute-worker leases";
    platforms = platforms.linux;
    mainProgram = "homelab-native-hardware";
  };
}
