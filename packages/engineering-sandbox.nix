{ pkgs }:
pkgs.stdenv.mkDerivation {
  pname = "homelab-engineering-sandbox";
  version = "0.1.0";
  src = ../services/sandbox;
  nativeBuildInputs = [ pkgs.rustc pkgs.makeWrapper ];
  buildPhase = ''
    runHook preBuild
    rustc --edition=2021 -D warnings -C opt-level=2 src/main.rs -o homelab-engineering-sandbox
    runHook postBuild
  '';
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    rustc --edition=2021 -D warnings --test src/main.rs -o sandbox-tests
    ./sandbox-tests
    runHook postCheck
  '';
  installPhase = ''
    runHook preInstall
    install -Dm755 homelab-engineering-sandbox "$out/bin/homelab-engineering-sandbox"
    wrapProgram "$out/bin/homelab-engineering-sandbox" \
      --set HOMELAB_SANDBOX_GIT "${pkgs.git}/bin/git" \
      --set HOMELAB_SANDBOX_BWRAP "${pkgs.bubblewrap}/bin/bwrap" \
      --set HOMELAB_SANDBOX_CARGO "${pkgs.cargo}/bin/cargo" \
      --set HOMELAB_SANDBOX_GO "${pkgs.go}/bin/go" \
      --set HOMELAB_SANDBOX_NIX "${pkgs.nix}/bin/nix"
    runHook postInstall
  '';
  meta = with pkgs.lib; {
    description = "Revision-bound isolated local build/test execution";
    platforms = platforms.linux;
    mainProgram = "homelab-engineering-sandbox";
  };
}
