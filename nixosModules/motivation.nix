{ config, lib, ... }:
let
  cfg = config.services.homelabMotivation;
in {
  options.services.homelabMotivation = {
    enable = lib.mkEnableOption "the private owner-directed motivation service";
    package = lib.mkOption {
      type = lib.types.package;
      description = "Immutable, verified Rust motivation service package with the homelab-motivation executable.";
    };
    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8341";
      description = "Numeric loopback listener for the private service.";
    };
    tokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected external file containing the service-only bearer token.";
    };
  };
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.tokenFile != null;
        message = "Motivation service requires an independent, protected token file.";
      }
      {
        assertion =
          let
            address = cfg.listenAddress;
            port = if lib.hasPrefix "127.0.0.1:" address then
              lib.removePrefix "127.0.0.1:" address
            else if lib.hasPrefix "[::1]:" address then
              lib.removePrefix "[::1]:" address
            else "";
          in port != "" && builtins.match "[0-9]+" port != null;
        message = "Motivation service must listen on an explicit numeric loopback address.";
      }
    ];
    systemd.services.homelab-motivation = {
      description = "Owner-directed motivation and engagement service";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = {
        HOMELAB_MOTIVATION_LISTEN_ADDR = cfg.listenAddress;
        HOMELAB_MOTIVATION_TOKEN_FILE = "/run/credentials/homelab-motivation.service/motivation-token";
      };
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        StateDirectory = "homelab-motivation";
        StateDirectoryMode = "0700";
        LoadCredential = [ "motivation-token:${cfg.tokenFile}" ];
        ExecStart = "${cfg.package}/bin/homelab-motivation";
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
        IPAddressDeny = "any";
        IPAddressAllow = "localhost";
      };
    };
  };
}
