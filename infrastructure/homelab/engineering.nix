{ config, lib, ... }:
{
  services.homelabEngineering = {
    enable = false;
    computeURL = "http://127.0.0.1:8106/mcp";
    computeTokenFile = null;
    oidc = {
      tokenURL = "https://identity.dbontr.me/oauth2/token";
      clientID = "homelab-engineering";
      clientSecretFile = "/srv/homelab/secrets/engineering-oidc-client-secret";
    };
    grantScopedProjects = true;
    allowedProjects = [];
    nativeVerification = false;
  };

  services.homelabPlatform.engineeringSocket =
    if config.services.homelabEngineering.enable
    then "/run/homelab-engineeringd/engineering.sock"
    else null;

  services.homelabExecutor.healthServices =
    lib.optionals config.services.homelabEngineering.enable
      [ "homelab-engineeringd.service" ];

  systemd.services.homelab-engineeringd =
    lib.mkIf config.services.homelabEngineering.enable {
      after = [ "nginx.service" "homelab-identityd.service" "cloudflared-saturn.service" ];
      wants = [ "cloudflared-saturn.service" ];
      requires = [ "homelab-identityd.service" ];
    };
}
