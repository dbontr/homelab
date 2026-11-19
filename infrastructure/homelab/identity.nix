{ pkgs, ... }:
let
  issuer = "https://identity.dbontr.me";
  audience = "homelab-compute";
  client = "homelab-engineering";
  secret = "/srv/homelab/secrets/engineering-oidc-client-secret";
  key = "/srv/homelab/secrets/security/engineering-machine-signing.pem";
  package = import ../../packages/identity.nix { inherit pkgs; };
in {
  systemd.services.homelab-identityd = {
    description = "Restricted OAuth2 machine identity issuer";
    wantedBy = [ "multi-user.target" ];
    after = [ "network-online.target" ];
    wants = [ "network-online.target" ];
    environment = {
      HOMELAB_IDENTITY_ISSUER = issuer;
      HOMELAB_IDENTITY_AUDIENCE = audience;
      HOMELAB_IDENTITY_CLIENT_ID = client;
      HOMELAB_IDENTITY_KEY_FILE = "/run/credentials/homelab-identityd.service/signing-key";
      HOMELAB_IDENTITY_SECRET_FILE = "/run/credentials/homelab-identityd.service/client-secret";
    };
    serviceConfig = {
      Type = "simple";
      ExecStart = "${package}/bin/homelab-identityd";
      DynamicUser = true;
      User = "homelab-identityd";
      LoadCredential = [
        "signing-key:${key}"
        "client-secret:${secret}"
      ];
      Restart = "on-failure";
      RestartSec = "5s";
      UMask = "0077";
      NoNewPrivileges = true;
      PrivateTmp = true;
      PrivateDevices = true;
      ProtectHome = true;
      ProtectSystem = "strict";
      ProtectKernelTunables = true;
      ProtectKernelModules = true;
      ProtectControlGroups = true;
      CapabilityBoundingSet = "";
      RestrictAddressFamilies = [ "AF_INET" "AF_UNIX" ];
      RestrictSUIDSGID = true;
      RestrictNamespaces = true;
      LockPersonality = true;
    };
  };
  services.workspaceMcp.oidc = {
    inherit issuer audience;
    jwksURL = "${issuer}/.well-known/jwks.json";
  };
}
