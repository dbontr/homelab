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

  # Preview only. Keep the legacy homepage active until console session
  # enrollment and browser-origin acceptance are complete.
  services.homelabConsole = {
    enable = true;
    replaceHomepage = false;
    package = pkgs.fetchFromGitHub {
      owner = "dbontr";
      repo = "homelab";
      rev = "7eb184f13b0448e1683b6ce76b97849f70663a42";
      sha256 = "17mlvisuiy7dh94gri9jvmrjn37iz92il0i0xk5a3f89g570h9i7";
    };
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
    package = pkgs.fetchFromGitHub {
      owner = "dbontr";
      repo = "homelab";
      rev = "90b09f41f363ba406337993cb7ced677ea247bdf";
      hash = "sha256-3wCdgUYirDq9PIVyudZ57sGToYiotyu3dJtdUAHK8tk=";
    };
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
