{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabFleet;
  package = import ../packages/fleet.nix { inherit pkgs; };
  declaredHosts = map (host: {
    inherit (host) host role endpoint;
    server_name = host.serverName;
  }) cfg.hosts;
in {
  options.services.homelabFleet = {
    enable = lib.mkEnableOption "independently authorized fleet release coordinator";
    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      description = "Native Go fleet release daemon.";
    };
    certificateFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected coordinator TLS client certificate.";
    };
    keyFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected coordinator TLS private key.";
    };
    hostCAFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Trusted CA roots for host release executors.";
    };
    hosts = lib.mkOption {
      default = [];
      description = "Fixed compute-first, control-last release target inventory.";
      type = lib.types.listOf (lib.types.submodule {
        options = {
          host = lib.mkOption { type = lib.types.str; };
          role = lib.mkOption { type = lib.types.enum [ "compute" "control" ]; };
          endpoint = lib.mkOption { type = lib.types.str; };
          serverName = lib.mkOption { type = lib.types.str; };
        };
      });
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = config.services.homelabSecurity.enable &&
          config.services.homelabSecurity.access.gatewayAudience != "";
        message = "Fleet release requires independent signed owner authorization on the coordinator host.";
      }
      {
        assertion = cfg.certificateFile != null && cfg.keyFile != null && cfg.hostCAFile != null;
        message = "Fleet release requires protected TLS client credentials and host trust roots.";
      }
      {
        assertion = cfg.hosts != [] && lib.length cfg.hosts <= 16 &&
          (lib.last cfg.hosts).role == "control" &&
          lib.all (host: host.role == "compute") (lib.init cfg.hosts) &&
          lib.length (lib.unique (map (host: host.host) cfg.hosts)) == lib.length cfg.hosts;
        message = "Fleet release requires distinct compute-first hosts and exactly one final control host.";
      }
      {
        assertion = lib.all (host:
          lib.hasPrefix "https://" host.endpoint && host.serverName != "" && host.host != ""
        ) cfg.hosts;
        message = "Fleet hosts require HTTPS release endpoints and exact TLS identities.";
      }
    ];
    users.groups.homelab-fleet-client = {};

    systemd.services.homelab-fleetd = {
      description = "Authenticated durable fleet release coordinator";
      wantedBy = [ "multi-user.target" ];
      requires = [ "homelab-securityd.service" ];
      after = [ "homelab-securityd.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = {
        HOMELAB_FLEET_CERT = "/run/credentials/homelab-fleetd.service/client-cert";
        HOMELAB_FLEET_KEY = "/run/credentials/homelab-fleetd.service/client-key";
        HOMELAB_FLEET_CA = "/run/credentials/homelab-fleetd.service/host-ca";
        HOMELAB_FLEET_HOSTS_FILE =
          pkgs.writeText "homelab-fleet-hosts.json" (builtins.toJSON declaredHosts);
        HOMELAB_FLEET_SECURITY_SOCKET = "/run/homelab-securityd/client.sock";
        HOMELAB_FLEET_STATE = "/var/lib/homelab-fleetd/fleet.json";
        HOMELAB_FLEET_PLAN = "/var/lib/homelab-fleetd/plan.json";
      };
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        SupplementaryGroups = [ "homelab-fleet-client" "homelab-security-client" ];
        StateDirectory = "homelab-fleetd";
        StateDirectoryMode = "0700";
        RuntimeDirectory = "homelab-fleetd";
        RuntimeDirectoryMode = "0750";
        LoadCredential = [
          "client-cert:${cfg.certificateFile}"
          "client-key:${cfg.keyFile}"
          "host-ca:${cfg.hostCAFile}"
        ];
        ExecStart = "${cfg.package}/bin/homelab-fleetd";
        Restart = "on-failure";
        RestartSec = "5s";
        UMask = "0077";
        NoNewPrivileges = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectControlGroups = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictSUIDSGID = true;
        RestrictNamespaces = true;
        LockPersonality = true;
      };
    };
  };
}
