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
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.computeTokenFile != null && cfg.computeURL != "";
        message = "Engineering requires a separate compute service credential and endpoint.";
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
        HOMELAB_COMPUTE_URL = cfg.computeURL;
        HOMELAB_COMPUTE_TOKEN_FILE =
          "/run/credentials/homelab-engineeringd.service/compute-token";
      } // lib.optionalAttrs (cfg.model.url != "") {
        HOMELAB_ENGINEERING_MODEL_URL = cfg.model.url;
        HOMELAB_ENGINEERING_MODEL_NAME = cfg.model.name;
      } // lib.optionalAttrs (cfg.model.tokenFile != null) {
        HOMELAB_ENGINEERING_MODEL_TOKEN_FILE =
          "/run/credentials/homelab-engineeringd.service/model-token";
      };
      serviceConfig = {
        Type = "simple";
        User = "homelab-engineering";
        Group = "homelab-engineering-client";
        StateDirectory = "homelab-engineeringd";
        StateDirectoryMode = "0700";
        RuntimeDirectory = "homelab-engineeringd";
        RuntimeDirectoryMode = "0750";
        LoadCredential = [ "compute-token:${cfg.computeTokenFile}" ]
          ++ lib.optional (cfg.model.tokenFile != null) "model-token:${cfg.model.tokenFile}";
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
