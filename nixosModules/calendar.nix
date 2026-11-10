{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabCalendar;
  defaultPackage = import ../packages/calendar.nix { inherit pkgs; };
  feedFile = if cfg.feedURLFile == null then "/dev/null" else cfg.feedURLFile;
  tokenFile = if cfg.tokenFile == null then "/dev/null" else cfg.tokenFile;
in {
  options.services.homelabCalendar = {
    enable = lib.mkEnableOption "the independent read-only calendar service";
    package = lib.mkOption {
      type = lib.types.package;
      default = defaultPackage;
      description = "Calendar service executable.";
    };
    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8420";
      description = "Private loopback listener for calendar reads.";
    };
    feedURLFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected file containing the HTTPS calendar sharing URL.";
    };
    tokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Dedicated calendar-service token shared only with authorized local clients.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.feedURLFile != null && cfg.tokenFile != null &&
          cfg.feedURLFile != cfg.tokenFile;
        message = "Calendar service requires independent protected source and service token files.";
      }
      {
        assertion = builtins.match "^(127\\.0\\.0\\.1|\\[::1\\]):[0-9]+$" cfg.listenAddress != null;
        message = "Calendar service must listen on an explicit numeric loopback address.";
      }
    ];
    systemd.services.homelab-calendar = {
      description = "Read-only personal calendar source";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = {
        HOMELAB_CALENDAR_LISTEN_ADDR = cfg.listenAddress;
        HOMELAB_CALENDAR_URL_FILE = "/run/credentials/homelab-calendar.service/feed-url";
        HOMELAB_CALENDAR_TOKEN_FILE = "/run/credentials/homelab-calendar.service/calendar-token";
      };
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        ExecStart = "${cfg.package}/bin/homelab-calendard";
        LoadCredential = [
          "feed-url:${feedFile}"
          "calendar-token:${tokenFile}"
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
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
      };
    };
  };
}
