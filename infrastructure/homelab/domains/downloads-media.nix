{ ... }:
{
  imports = [
    ../media-tailnet.nix
    ../media-local.nix
    ../media-proxy.nix
    ../downloads.nix
  ];

  homelab.native.downloads.enable = true;
  homelab.native.applications.media.enable = true;
  homelab.native.applications.seerr.enable = true;
  homelab.native.applications.dispatcharr.enable = false;
}
