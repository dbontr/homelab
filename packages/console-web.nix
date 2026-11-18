{ pkgs }:
pkgs.stdenvNoCC.mkDerivation {
  pname = "homelab-console-web";
  version = "0.1.0";
  src = ../ui/web;

  nativeBuildInputs = [
    pkgs.nodejs_22
    pkgs.importNpmLock.hooks.linkNodeModulesHook
  ];
  npmDeps = pkgs.importNpmLock.buildNodeModules {
    npmRoot = ../ui/web;
    nodejs = pkgs.nodejs_22;
  };

  buildPhase = ''
    runHook preBuild
    export HOME="$TMPDIR"
    npm run build
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall
    mkdir -p "$out"
    cp -a dist/. "$out/"
    test -s "$out/index.html"
    test -n "$(find "$out/assets" -maxdepth 1 -name '*.js' -type f -print -quit)"
    runHook postInstall
  '';
}
