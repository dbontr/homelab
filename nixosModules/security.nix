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
      { assertion = cfg.owner != ""; message = "Security daemon requires an owner identity."; }
      { assertion = cfg.passwordVerifierFile != null && cfg.totpSecretFile != null;
        message = "Security daemon requires separate password and TOTP credential files."; }
      { assertion = cfg.policies != []; message = "Security daemon requires explicit operation and resource policy."; }
      { assertion = lib.all (p: p.operation != "" && p.targets != []) cfg.policies;
        message = "Security policies must name an operation and at least one target."; }
    ];
    users.groups.homelab-security = {};
    users.groups.homelab-security-client = {};
    users.users.homelab-security = {
      isSystemUser = true;
      group = "homelab-security";
      extraGroups = [ "homelab-security-client" ];
      home = "/var/lib/homelab-securityd";
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
      };
      serviceConfig = {
        Type = "simple";
        User = "homelab-security";
        Group = "homelab-security";
        SupplementaryGroups = [ "homelab-security-client" ];
        StateDirectory = "homelab-securityd";
        StateDirectoryMode = "0700";
        RuntimeDirectory = "homelab-securityd";
        RuntimeDirectoryMode = "0750";
        LoadCredential = [
          "password:${cfg.passwordVerifierFile}"
          "totp:${cfg.totpSecretFile}"
        ];
        ExecStart = "${cfg.package}/bin/homelab-securityd";
        Restart = "on-failure";
        RestartSec = "5s";
        UMask = "0077";
        NoNewPrivileges = true;
        CapabilityBoundingSet = "";
        PrivateTmp = true;
        PrivateNetwork = true;
        PrivateDevices = true;
        ProtectHome = true;
        ProtectSystem = "strict";
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        ProtectClock = true;
        RestrictAddressFamilies = [ "AF_UNIX" ];
        RestrictNamespaces = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
      };
    };
  };
}
