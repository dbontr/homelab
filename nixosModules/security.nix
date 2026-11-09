{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabSecurity;
  package = import ../packages/security.nix { inherit pkgs; };
in
{
  options.services.homelabSecurity = {
    enable = lib.mkEnableOption "independent password and TOTP privileged-session service";
    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      description = "Native Go security daemon and trusted local administrator CLI.";
    };
    owner = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Local administrator identity. Must not be an MCP-controlled parameter.";
    };
    passwordVerifierFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Private path containing the PBKDF2 password verifier JSON.";
    };
    totpSecretFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Private path containing the Base32 TOTP secret.";
    };
    vaultKeyFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Path to the 32-byte owner-controlled vault key, loaded only into the security daemon.";
    };
    access = {
      issuer = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Expected Cloudflare Access issuer.";
      };
      gatewayAudience = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Audience of the authenticated gateway application.";
      };
      ownerEmailFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected owner identity file; never loaded into the MCP.";
      };
      jwksFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Pinned issuer signing JWKS file, updated through the trusted release channel.";
      };
    };
    remote = {
      enable = lib.mkEnableOption "mutually authenticated authorizer for host executors";
      listenAddress = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Explicit private IP and TCP port for remote authorization.";
      };
      certificateFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected authorization server certificate.";
      };
      keyFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected authorization server private key.";
      };
      clientCAFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "CA roots permitted to sign remote executor client certificates.";
      };
      allowedClientDNS = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [];
        description = "Exact certificate DNS identities permitted to request operation authorization.";
      };
      serviceCredentialGrants = lib.mkOption {
        default = [];
        description = "Exact service identity and opaque vault handle bindings for mutually authenticated credential reads. Never put credential values in this configuration.";
        type = lib.types.listOf (lib.types.submodule {
          options = {
            clientDNS = lib.mkOption { type = lib.types.str; };
            alias = lib.mkOption { type = lib.types.str; };
            provider = lib.mkOption { type = lib.types.str; };
            handle = lib.mkOption { type = lib.types.str; };
          };
        });
      };
    };
    oauth = {
      enable = lib.mkEnableOption "owner-only OAuth authorization-code/PKCE or device-code enrollment";
      providers = lib.mkOption {
        default = [];
        description = "Allowlisted OAuth public clients; either a fixed loopback authorization callback or a provider-specific device authorization URL with an exact verification hostname. No provider secrets.";
        type = lib.types.listOf (lib.types.submodule {
          options = {
            name = lib.mkOption { type = lib.types.str; };
            authorizeURL = lib.mkOption { type = lib.types.str; default = ""; };
            tokenURL = lib.mkOption { type = lib.types.str; };
            clientID = lib.mkOption { type = lib.types.str; };
            redirectURL = lib.mkOption { type = lib.types.str; default = ""; };
            deviceAuthorizationURL = lib.mkOption { type = lib.types.str; default = ""; };
            deviceVerificationHost = lib.mkOption { type = lib.types.str; default = ""; };
            scopes = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
          };
        });
      };
    };
    browser = {
      enable = lib.mkEnableOption "owner-only HTTPS browser login reached through an independently protected ingress";
      hostname = lib.mkOption {
        type = lib.types.str;
        default = "auth.dbontr.me";
        description = "Exact HTTPS hostname for the owner credential portal.";
      };
      audience = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Dedicated Cloudflare Access audience for the owner-only authentication application.";
      };
    };
    policies = lib.mkOption {
      default = [];
      description = "Explicitly permitted operation/resource identities. Every destructive operation needs local owner confirmation.";
      type = lib.types.listOf (lib.types.submodule {
        options = {
          operation = lib.mkOption { type = lib.types.str; };
          targets = lib.mkOption { type = lib.types.listOf lib.types.str; };
          destructive = lib.mkOption { type = lib.types.bool; default = false; };
        };
      });
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      { assertion = !cfg.remote.enable ||
          (cfg.remote.listenAddress != null && cfg.remote.certificateFile != null &&
           cfg.remote.keyFile != null && cfg.remote.clientCAFile != null &&
           cfg.remote.allowedClientDNS != [] && cfg.access.gatewayAudience != "");
        message = "Remote authorization needs exact client identities, TLS credentials, a private listener and signed owner verification."; }
      { assertion = cfg.remote.serviceCredentialGrants == [] ||
          (cfg.remote.enable && cfg.vaultKeyFile != null &&
           lib.all (grant: lib.elem grant.clientDNS cfg.remote.allowedClientDNS) cfg.remote.serviceCredentialGrants);
        message = "Service credentials require independent vault custody, private remote TLS, and an explicitly allowed client certificate identity."; }
      { assertion = cfg.owner != ""; message = "Security daemon requires an owner identity."; }
      { assertion = cfg.passwordVerifierFile != null && cfg.totpSecretFile != null;
        message = "Security daemon requires separate password and TOTP credential files."; }
      { assertion = cfg.policies != []; message = "Security daemon requires explicit operation and resource policy."; }
      { assertion = lib.all (p: p.operation != "" && p.targets != []) cfg.policies;
        message = "Security policies must name an operation and at least one target."; }
      { assertion = !cfg.oauth.enable ||
          (cfg.vaultKeyFile != null && cfg.oauth.providers != []);
        message = "OAuth requires a protected credential vault key and an explicit public-client provider registry."; }
      { assertion = !cfg.browser.enable || (cfg.browser.audience != null && cfg.browser.audience != "");
        message = "Browser login requires its own configured Cloudflare Access audience."; }
      { assertion = (cfg.access.gatewayAudience == "" && cfg.access.issuer == "" &&
          cfg.access.ownerEmailFile == null && cfg.access.jwksFile == null) ||
          (cfg.access.gatewayAudience != "" && cfg.access.issuer != "" &&
          cfg.access.ownerEmailFile != null && cfg.access.jwksFile != null);
        message = "Cloudflare identity verification requires issuer, gateway audience, owner identity and signing keys together."; }
      { assertion = !cfg.browser.enable || cfg.access.gatewayAudience != "";
        message = "Browser login requires independently verified Access identity."; }
      { assertion = !cfg.browser.enable || cfg.browser.audience != cfg.access.gatewayAudience;
        message = "Browser and gateway Access audiences must be distinct."; }
      { assertion = !cfg.browser.enable || config.services.nginx.enable;
        message = "Browser login requires private nginx ingress."; }
    ];
    users.groups.homelab-security = {};
    users.groups.homelab-security-client = {};
    users.groups.homelab-security-browser = lib.mkIf cfg.browser.enable {};
    users.users.homelab-security = {
      isSystemUser = true;
      group = "homelab-security";
      extraGroups = [ "homelab-security-client" ] ++ lib.optional cfg.browser.enable "homelab-security-browser";
      home = "/var/lib/homelab-securityd";
    };
    users.users.nginx.extraGroups = lib.mkIf cfg.browser.enable [ "homelab-security-browser" ];
    services.nginx.virtualHosts = lib.mkIf cfg.browser.enable {
      "${cfg.browser.hostname}" = {
        listen = [ { addr = "127.0.0.1"; port = 8115; } ];
        extraConfig = "client_max_body_size 4k; server_tokens off;";
        locations."/".extraConfig = ''
          proxy_pass http://unix:/run/homelab-security-browser/browser.sock:/;
          proxy_set_header Host $host;
          proxy_set_header X-Forwarded-Proto https;
          proxy_set_header Cf-Access-Jwt-Assertion $http_cf_access_jwt_assertion;
          proxy_http_version 1.1;
          proxy_read_timeout 15s;
          proxy_send_timeout 15s;
        '';
      };
    };
    environment.systemPackages = [ cfg.package ];
    systemd.services.homelab-securityd = {
      description = "Independent owner authentication and privileged-session policy";
      wantedBy = [ "multi-user.target" ];
      environment = {
        HOMELAB_SECURITY_OWNER = cfg.owner;
        HOMELAB_SECURITY_PASSWORD_FILE = "/run/credentials/homelab-securityd.service/password";
        HOMELAB_SECURITY_TOTP_FILE = "/run/credentials/homelab-securityd.service/totp";
        HOMELAB_SECURITY_POLICY_FILE = pkgs.writeText "homelab-security-policy.json" (builtins.toJSON cfg.policies);
      } // lib.optionalAttrs cfg.remote.enable {
        HOMELAB_SECURITY_REMOTE_LISTEN = cfg.remote.listenAddress;
        HOMELAB_SECURITY_REMOTE_CERT = "/run/credentials/homelab-securityd.service/remote-cert";
        HOMELAB_SECURITY_REMOTE_KEY = "/run/credentials/homelab-securityd.service/remote-key";
        HOMELAB_SECURITY_REMOTE_CLIENT_CA = "/run/credentials/homelab-securityd.service/remote-client-ca";
        HOMELAB_SECURITY_REMOTE_CLIENT_IDENTITIES = builtins.toJSON cfg.remote.allowedClientDNS;
        HOMELAB_SECURITY_SERVICE_CREDENTIAL_GRANTS = if cfg.remote.serviceCredentialGrants == []
          then "" else builtins.toJSON (map (grant: {
            client_dns = grant.clientDNS;
            alias = grant.alias;
            provider = grant.provider;
            handle = grant.handle;
          }) cfg.remote.serviceCredentialGrants);
      } // lib.optionalAttrs cfg.oauth.enable {
        HOMELAB_SECURITY_OAUTH_PROVIDERS = builtins.toJSON cfg.oauth.providers;
      } // lib.optionalAttrs cfg.browser.enable {
        HOMELAB_SECURITY_BROWSER_ORIGIN = "https://${cfg.browser.hostname}";
        HOMELAB_SECURITY_ACCESS_BROWSER_AUD = cfg.browser.audience;
      } // lib.optionalAttrs (cfg.vaultKeyFile != null) {
        HOMELAB_SECURITY_VAULT_KEY_FILE = "/run/credentials/homelab-securityd.service/vault-key";
      } // lib.optionalAttrs (cfg.access.gatewayAudience != "") {
        HOMELAB_SECURITY_ACCESS_ISSUER = cfg.access.issuer;
        HOMELAB_SECURITY_ACCESS_GATEWAY_AUD = cfg.access.gatewayAudience;
        HOMELAB_SECURITY_ACCESS_JWKS_FILE = "/run/credentials/homelab-securityd.service/access-jwks";
        HOMELAB_SECURITY_ACCESS_OWNER_FILE = "/run/credentials/homelab-securityd.service/access-owner";
      };
      serviceConfig = {
        Type = "simple";
        User = "homelab-security";
        Group = "homelab-security";
        SupplementaryGroups = [ "homelab-security-client" ] ++ lib.optional cfg.browser.enable "homelab-security-browser";
        StateDirectory = "homelab-securityd";
        StateDirectoryMode = "0700";
        RuntimeDirectory = [ "homelab-securityd" ] ++ lib.optional cfg.browser.enable "homelab-security-browser";
        RuntimeDirectoryMode = "0750";
        LoadCredential = [
          "password:${cfg.passwordVerifierFile}"
          "totp:${cfg.totpSecretFile}"
        ] ++ lib.optionals cfg.remote.enable [
          "remote-cert:${cfg.remote.certificateFile}"
          "remote-key:${cfg.remote.keyFile}"
          "remote-client-ca:${cfg.remote.clientCAFile}"
        ] ++ lib.optionals (cfg.vaultKeyFile != null) [
          "vault-key:${cfg.vaultKeyFile}"
        ] ++ lib.optionals (cfg.access.gatewayAudience != "") [
          "access-jwks:${cfg.access.jwksFile}"
          "access-owner:${cfg.access.ownerEmailFile}"
        ];
        ExecStart = "${cfg.package}/bin/homelab-securityd";
        Restart = "on-failure";
        RestartSec = "5s";
        UMask = "0077";
        NoNewPrivileges = true;
        CapabilityBoundingSet = "";
        PrivateTmp = true;
        PrivateNetwork = !(cfg.remote.enable || cfg.oauth.enable);
        PrivateDevices = true;
        ProtectHome = true;
        ProtectSystem = "strict";
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        ProtectClock = true;
        RestrictAddressFamilies = [ "AF_UNIX" ] ++ lib.optionals (cfg.remote.enable || cfg.oauth.enable) [ "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
      };
    };
  };
}
