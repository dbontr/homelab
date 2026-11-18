{ ... }:
{
  services.homelabEngineering = {
    enable = true;
    # This identity must be a non-administrator OIDC principal with an exact
    # project editor grant. The controller's owner token is not permitted.
    computeURL = "http://127.0.0.1:8106/mcp";
    computeTokenFile = "/srv/homelab/secrets/engineering-compute-editor.token";
    grantScopedProjects = true;
    allowedProjects = [];
    nativeVerification = false;
  };

  # The gateway receives only a restricted Unix-socket client, never the
  # Compute editor credential or an unrestricted command execution path.
  services.homelabPlatform.engineeringSocket =
    "/run/homelab-engineeringd/engineering.sock";

  services.homelabExecutor.healthServices = [ "homelab-engineeringd.service" ];

  systemd.services.homelab-engineeringd.after = [ "nginx.service" ];
}
