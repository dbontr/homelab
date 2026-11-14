{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabEngineering;
  package = import ../packages/engineering.nix { inherit pkgs; };
in {
  options.services.homelabEngineering = {
    enable = lib.mkEnableOption "independent bounded engineering execution";
    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      description = "Native engineering service binary.";
    };
    computeURL = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Authenticated compute controller MCP endpoint.";
    };
    computeTokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Private compute-service credential available only to the engineering daemon.";
    };
    computeVaultAlias = lib.mkOption { type = lib.types.str; default = ""; description = "Vault alias for the non-administrator compute editor token."; };
    vault = {
      endpoint = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Private mTLS credential authority origin."; };
      serverName = lib.mkOption { type = lib.types.str; default = ""; description = "Expected vault TLS DNS identity."; };
      caFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Protected vault trust certificate."; };
      certificateFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Dedicated Engineering service client certificate."; };
      keyFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Dedicated Engineering service private key."; };
    };
    nativeVerification = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Require typed, revision-bound verification in a separate Rust Kubernetes Job. The worker must have a pinned verifier image.";
    };
    allowedProjects = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Exact compute projects in which engineering runs may be submitted.";
    };
    model = {
      url = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Optional HTTPS or loopback chat-completions endpoint for scoped source generation.";
      };
      name = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Exact model identifier for source generation.";
      };
      tokenFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Optional protected model token; not available to the gateway.";
      };
      vaultAlias = lib.mkOption { type = lib.types.str; default = ""; description = "Owner vault alias for source-authoring model credential."; };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.computeVaultAlias == "" && cfg.model.vaultAlias == "" &&
          cfg.vault.endpoint == null;
        message = "Unattended engineering jobs require protected service credentials. Owner-bound vault tickets cannot be issued without a current signed human operation.";
      }
      {
        assertion = cfg.computeURL != "" &&
          ((cfg.computeTokenFile != null) != (cfg.computeVaultAlias != ""));
        message = "Engineering requires exactly one protected compute editor credential source.";
      }
      {
        assertion = cfg.allowedProjects != [] && lib.length cfg.allowedProjects <= 32
          && lib.length (lib.unique cfg.allowedProjects) == lib.length cfg.allowedProjects
          && lib.all (project: builtins.match "^[a-z0-9][a-z0-9_-]{0,79}$" project != null) cfg.allowedProjects;
        message = "Engineering requires a finite list of unique approved project identifiers.";
      }
      {
        assertion = (cfg.model.url == "") || (cfg.model.name != "");
        message = "Model source generation requires a selected model name.";
      }
      {
        assertion = (cfg.model.tokenFile == null || cfg.model.vaultAlias == "") &&
          (cfg.model.url != "" || (cfg.model.tokenFile == null && cfg.model.vaultAlias == ""));
        message = "The Engineering model must use one credential source with a configured endpoint.";
      }
      {
        assertion = let needsVault = cfg.computeVaultAlias != "" || cfg.model.vaultAlias != "";
        in (!needsVault && cfg.vault.endpoint == null) ||
          (cfg.vault.endpoint != null && cfg.vault.serverName != "" &&
           cfg.vault.caFile != null && cfg.vault.certificateFile != null &&
           cfg.vault.keyFile != null);
        message = "Engineering vault mode requires a pinned server identity and protected mutual TLS files.";
      }
    ];

    users.groups.homelab-engineering-client = {};
    users.users.homelab-engineering = {
      isSystemUser = true;
      group = "homelab-engineering-client";
      home = "/var/lib/homelab-engineeringd";
    };

    systemd.services.homelab-engineeringd = {
      description = "Isolated engineering run scheduler";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = {
        HOMELAB_ENGINEERING_SOCKET = "/run/homelab-engineeringd/engineering.sock";
        HOMELAB_ENGINEERING_DIR = "/var/lib/homelab-engineeringd/runs";
        HOMELAB_ENGINEERING_PROJECTS = lib.concatStringsSep "," cfg.allowedProjects;
        HOMELAB_ENGINEERING_NATIVE_VERIFICATION = if cfg.nativeVerification then "1" else "0";
        HOMELAB_COMPUTE_URL = cfg.computeURL;
      } // lib.optionalAttrs (cfg.computeTokenFile != null) {
        HOMELAB_COMPUTE_TOKEN_FILE = "/run/credentials/homelab-engineeringd.service/compute-token";
      } // lib.optionalAttrs (cfg.computeVaultAlias != "") {
        HOMELAB_ENGINEERING_COMPUTE_VAULT_ALIAS = cfg.computeVaultAlias;
      } // lib.optionalAttrs (cfg.vault.endpoint != null) {
        HOMELAB_ENGINEERING_VAULT_URL = cfg.vault.endpoint;
        HOMELAB_ENGINEERING_VAULT_SERVER_NAME = cfg.vault.serverName;
        HOMELAB_ENGINEERING_VAULT_CA_FILE = "/run/credentials/homelab-engineeringd.service/vault-ca";
        HOMELAB_ENGINEERING_VAULT_CLIENT_CERT_FILE = "/run/credentials/homelab-engineeringd.service/vault-cert";
        HOMELAB_ENGINEERING_VAULT_CLIENT_KEY_FILE = "/run/credentials/homelab-engineeringd.service/vault-key";
      } // lib.optionalAttrs (cfg.model.url != "") {
        HOMELAB_ENGINEERING_MODEL_URL = cfg.model.url;
        HOMELAB_ENGINEERING_MODEL_NAME = cfg.model.name;
      } // lib.optionalAttrs (cfg.model.tokenFile != null) {
        HOMELAB_ENGINEERING_MODEL_TOKEN_FILE =
          "/run/credentials/homelab-engineeringd.service/model-token";
      } // lib.optionalAttrs (cfg.model.vaultAlias != "") {
        HOMELAB_ENGINEERING_MODEL_VAULT_ALIAS = cfg.model.vaultAlias;
      };
      serviceConfig = {
        Type = "simple";
        User = "homelab-engineering";
        Group = "homelab-engineering-client";
        StateDirectory = "homelab-engineeringd";
        StateDirectoryMode = "0700";
        RuntimeDirectory = "homelab-engineeringd";
        RuntimeDirectoryMode = "0750";
        LoadCredential = lib.optional (cfg.computeTokenFile != null) "compute-token:${cfg.computeTokenFile}"
          ++ lib.optional (cfg.model.tokenFile != null) "model-token:${cfg.model.tokenFile}"
          ++ lib.optionals (cfg.vault.endpoint != null) [
            "vault-ca:${cfg.vault.caFile}"
            "vault-cert:${cfg.vault.certificateFile}"
            "vault-key:${cfg.vault.keyFile}"
          ];
        ExecStart = "${cfg.package}/bin/homelab-engineeringd";
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
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
        RestrictSUIDSGID = true;
        RestrictNamespaces = true;
        LockPersonality = true;
      };
    };
  };
}
