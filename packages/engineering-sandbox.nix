{ pkgs }:
pkgs.buildGo127Module {
  pname = "homelab-engineering-sandbox";
  version = "0.1.0";
  src = ../services/sandbox;
  vendorHash = null;
  subPackages = [ "." ];
  doCheck = true;
  nativeBuildInputs = [ pkgs.makeWrapper ];

  postInstall = ''
    mv "$out/bin/sandbox" "$out/bin/homelab-engineering-sandbox"
    wrapProgram "$out/bin/homelab-engineering-sandbox" \
      --set HOMELAB_SANDBOX_GIT "${pkgs.git}/bin/git" \
      --set HOMELAB_SANDBOX_BWRAP "${pkgs.bubblewrap}/bin/bwrap" \
      --set HOMELAB_SANDBOX_CARGO "${pkgs.cargo}/bin/cargo" \
      --set HOMELAB_SANDBOX_GO "${pkgs.go_1_27}/bin/go" \
      --set HOMELAB_SANDBOX_NIX "${pkgs.nix}/bin/nix"
    test -x "$out/bin/homelab-engineering-sandbox"
  '';

  meta = with pkgs.lib; {
    description = "Isolated, revision-bound engineering verification";
    platforms = platforms.linux;
    mainProgram = "homelab-engineering-sandbox";
  };
}
