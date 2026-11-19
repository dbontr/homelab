{ config, pkgs, ... }:
{
  # Each listener owns its own credential and runs on loopback.
  services.homelabPlatform = {
    enable = true;
    tokenFile = "/srv/homelab/secrets/homelab-platform.token";
    securityClientSocket = "/run/homelab-securityd/client.sock";
    securityLoginURL = "https://auth.dbontr.me";
    # Compute discovery and execution forward only independently signed,
    # owner-verified Cloudflare Access assertions; the platform never holds
    # the controller's administrator bearer credential.
    integrations.workspace = {
      url = "http://127.0.0.1:8104/workspace";
      forwardOwnerAssertion = true;
    };
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

  # Serve the authenticated first-party console on the private home origin.
  services.homelabConsole = {
    enable = true;
    replaceHomepage = true;
    package = import ../../packages/console-web.nix { inherit pkgs; };
  };

  services.homelabInterfaces = {
    mcp = {
      enable = true;
      listenAddress = "127.0.0.1:8401";
      clientTokenFile = "/srv/homelab/secrets/homelab-mcp-client.token";
      serviceTokenFile = "/srv/homelab/secrets/homelab-mcp-service.token";
      allowedOrigins = [ "https://chatgpt.com" "https://chat.openai.com" ];
      access = {
        issuer = config.services.homelabSecurity.access.issuer;
        audience = config.services.homelabSecurity.access.gatewayAudience;
        ownerEmailFile = config.services.homelabSecurity.access.ownerEmailFile;
        jwksFile = config.services.homelabSecurity.access.jwksFile;
      };
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
