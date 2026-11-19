{ pkgs }:
let
  binary = pkgs.buildGo127Module {
    pname = "homelab-engineering-sandbox-bin";
    version = "0.1.0";
    src = ../services/sandbox;
    vendorHash = null;
    subPackages = [ "." ];
    doCheck = true;
    postInstall = ''
      mv "$out/bin/sandbox" "$out/bin/homelab-engineering-sandbox"
      test -x "$out/bin/homelab-engineering-sandbox"
    '';
  };
in pkgs.symlinkJoin {
  name = "homelab-engineering-sandbox";
  paths = [ binary ];
  nativeBuildInputs = [ pkgs.makeWrapper ];
  postBuild = ''
    wrapProgram "$out/bin/homelab-engineering-sandbox" \
      --set HOMELAB_SANDBOX_GIT "${pkgs.git}/bin/git" \
      --set HOMELAB_SANDBOX_BWRAP "${pkgs.bubblewrap}/bin/bwrap" \
      --set HOMELAB_SANDBOX_CARGO "${pkgs.cargo}/bin/cargo" \
      --set HOMELAB_SANDBOX_GO "${pkgs.go_1_27}/bin/go" \
      --set HOMELAB_SANDBOX_NIX "${pkgs.nix}/bin/nix"
  '';
  meta = with pkgs.lib; {
    description = "Isolated, revision-bound engineering verification";
    platforms = platforms.linux;
    mainProgram = "homelab-engineering-sandbox";
  };
}
