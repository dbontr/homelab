{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabPlatform;
  package = import ../packages/platform.nix { inherit pkgs; };
in
{
  options.services.homelabPlatform = {
    enable = lib.mkEnableOption "the integrated personal computing API and MCP gateway";
    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      description = "Go platform executable.";
    };
    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8300";
    };
    timeZone = lib.mkOption {
      type = lib.types.str;
      default = "America/Chicago";
    };
    calendarURLFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected file containing the read-only Proton Calendar sharing URL.";
    };
    tokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Path to the owner-only API token outside the Nix store.";
    };
    integrations = lib.genAttrs [ "workspace" "learning" "health" ] (_: {
      url = lib.mkOption { type = lib.types.str; default = ""; };
      tokenFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; };
    });
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.tokenFile != null;
        message = "The platform requires a protected API token file.";
      }
      {
        assertion = lib.hasPrefix "127.0.0.1:" cfg.listenAddress
          || lib.hasPrefix "[::1]:" cfg.listenAddress
          || lib.hasPrefix "localhost:" cfg.listenAddress;
        message = "The platform must listen on loopback behind authenticated ingress.";
      }
    ];

    systemd.services.homelab-platform = {
      description = "Personal computing platform";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = {
        HOMELAB_LISTEN_ADDR = cfg.listenAddress;
        HOMELAB_TIME_ZONE = cfg.timeZone;
        HOMELAB_STATE_FILE = "/var/lib/homelab-platform/state.json";
        HOMELAB_API_TOKEN_FILE = "/run/credentials/homelab-platform.service/access_token";
      } // lib.optionalAttrs (cfg.calendarURLFile != null) {
        HOMELAB_CALENDAR_URL_FILE = "/run/credentials/homelab-platform.service/calendar_url";
      } // (lib.foldl' (acc: name:
        let
          integration = cfg.integrations.${name};
          upper = lib.toUpper name;
        in acc
          // lib.optionalAttrs (integration.url != "") {
            "HOMELAB_${upper}_MCP_URL" = integration.url;
          }
          // lib.optionalAttrs (integration.tokenFile != null) {
            "HOMELAB_${upper}_MCP_TOKEN_FILE" =
              "/run/credentials/homelab-platform.service/${name}_token";
          }
      ) {} [ "workspace" "learning" "health" ]);
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        StateDirectory = "homelab-platform";
        StateDirectoryMode = "0700";
        LoadCredential =
          [ "access_token:${cfg.tokenFile}" ]
          ++ lib.optional (cfg.calendarURLFile != null) "calendar_url:${cfg.calendarURLFile}"
          ++ lib.concatMap (name:
            lib.optional (cfg.integrations.${name}.tokenFile != null)
              "${name}_token:${cfg.integrations.${name}.tokenFile}"
          ) [ "workspace" "learning" "health" ];
        ExecStart = "${cfg.package}/bin/homelab-platform";
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
      };
    };
  };
}
