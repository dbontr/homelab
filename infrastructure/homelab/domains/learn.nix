{ ... }:
{
  imports = [
    ../adaptive-teaching.nix
  ];

  homelab.learning = {
    enable = true;
    binaryPath = "/srv/releases/adaptive-teaching/access-20261106/bin/adaptive-teaching";
    binarySHA256 = "a732a7cdd9bac6e0232e4f10a77daa22a65d0027cfbf91dbe890d8d3830d3861";
    bearerCredentialFile = "/srv/homelab/secrets/adaptive-teaching.token";
    listenAddress = "127.0.0.1:8200";
    allowedHosts = "127.0.0.1:*,localhost:*";
    allowedOrigins = "https://chatgpt.com,https://chat.openai.com";
    access = {
      issuer = "https://dbontr.cloudflareaccess.com";
      audience = "85181fd33a10983cc64020e9155ffeeea8f9e4c6b7af6dd1a51f5c3588f63f25";
      certsURL = "https://dbontr.cloudflareaccess.com/cdn-cgi/access/certs";
      ownerCredentialFile = "/srv/homelab/secrets/adaptive-access-owner.email";
    };
  };
}
