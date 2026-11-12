{ config, pkgs, ... }:
{
  # Each listener owns its own credential and runs on loopback.
  services.homelabPlatform = {
    enable = true;
    tokenFile = "/srv/homelab/secrets/homelab-platform.token";
    securityClientSocket = "/run/homelab-securityd/client.sock";
    executorSocket = "/run/homelab-executord/execute.sock";
    consoleAccess = {
      issuer = config.services.homelabSecurity.access.issuer;
      audience = config.homelab.access.consoleAudTag;
      ownerEmailFile = config.services.homelabSecurity.access.ownerEmailFile;
      jwksFile = config.services.homelabSecurity.access.jwksFile;
    };
    allowedOrigins = [
      "https://dbontr.me"
      "https://chatgpt.com"
      "https://chat.openai.com"
    ];
  };

  services.homelabMotivation = {
    enable = true;
    package = import ../../packages/motivation.nix { inherit pkgs; };
    tokenFile = "/srv/homelab/secrets/homelab-motivation.token";
  };

  # Promote only the exact, source-bound console artifact. Keep the existing
  # homepage selected until owner login and browser API flows are accepted.
  services.homelabConsole = {
    enable = true;
    replaceHomepage = false;
    package = import ../../packages/console.nix { inherit pkgs; };
  };

  services.homelabInterfaces = {
    mcp = {
      enable = true;
      listenAddress = "127.0.0.1:8401";
      clientTokenFile = "/srv/homelab/secrets/homelab-mcp-client.token";
      serviceTokenFile = "/srv/homelab/secrets/homelab-mcp-service.token";
      allowedOrigins = [ "https://chatgpt.com" "https://chat.openai.com" ];
    };
    api = {
      enable = true;
      listenAddress = "127.0.0.1:8402";
      clientTokenFile = "/srv/homelab/secrets/homelab-api-client.token";
      serviceTokenFile = "/srv/homelab/secrets/homelab-api-service.token";
      allowedOrigins = [ "https://dbontr.me" ];
      writePaths = [];
    };
  };
}
