{ ... }:
{
  imports = [
    ../recipes-proxy.nix
    ../personal-health.nix
  ];

  homelab.native.applications.recipesage.enable = true;

  homelab.personalHealth = {
    enable = true;
    binaryPath = "/srv/releases/personal-health-mcp/a64580e41715a9c7b680c8ee3f186e9b1e550f5c/bin/personal-health-mcp";
    binarySHA256 = "6ca3a23a77e076df857ca1746f769a193b6d20aad76060be87124e4579737b97";
    access = {
      issuer = "https://dbontr.cloudflareaccess.com";
      audience = "85181fd33a10983cc64020e9155ffeeea8f9e4c6b7af6dd1a51f5c3588f63f25";
      certsURL = "https://dbontr.cloudflareaccess.com/cdn-cgi/access/certs";
      ownerCredentialFile = "/srv/homelab/secrets/adaptive-access-owner.email";
    };
  };
}
