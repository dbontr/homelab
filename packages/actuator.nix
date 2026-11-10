{ pkgs }:
pkgs.stdenv.mkDerivation {
  pname = "homelab-actuator";
  version = "0.1.0";
  src = ../services/actuator;
  nativeBuildInputs = [ pkgs.rustc ];
  buildPhase = ''
    runHook preBuild
    rustc --edition=2021 -D warnings -C opt-level=2 src/main.rs -o homelab-actuator
    runHook postBuild
  '';
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    rustc --edition=2021 -D warnings --test src/main.rs -o homelab-actuator-tests
    ./homelab-actuator-tests
    runHook postCheck
  '';
  installPhase = ''
    runHook preInstall
    install -Dm755 homelab-actuator "$out/bin/homelab-actuator"
    runHook postInstall
  '';
}
