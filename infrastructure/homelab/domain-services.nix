{ pkgs, ... }:
{
  # Each listener owns its own credential and runs on loopback.
  services.homelabPlatform = {
    enable = true;
    tokenFile = "/srv/homelab/secrets/homelab-platform.token";
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
