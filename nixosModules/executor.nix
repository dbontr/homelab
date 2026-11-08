{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabExecutor;
  package = import ../packages/executor.nix { inherit pkgs; };
in
{
  options.services.homelabExecutor = {
    enable = lib.mkEnableOption "restricted independent local executor";
    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      description = "Executor and guarded NixOS release controller.";
    };
    sourceDirectory = lib.mkOption {
      type = lib.types.str;
      default = "/srv/homelab-new";
      description = "Trusted local Git checkout used only by the offline release stager.";
    };
    stateDirectory = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/homelab-releases";
      description = "Private release journal and staged-generation metadata directory.";
    };
    allowReleaseActivation = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Explicitly allow an owner-confirmed local NixOS generation switch through the executor.";
    };
    allowedServices = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Exact local systemd unit names permitted for owner-approved restarts.";
    };
    healthServices = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Non-empty service readiness list required for activation and rollback.";
    };
  };
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = config.services.homelabSecurity.enable &&
          config.services.homelabSecurity.access.gatewayAudience != "";
        message = "Independent executor requires signed owner verification in homelab-securityd.";
      }
      {
        assertion = cfg.healthServices != [];
        message = "Independent NixOS releases require at least one health service.";
      }
      {
        assertion = lib.hasPrefix "/" cfg.sourceDirectory &&
          lib.hasPrefix "/" cfg.stateDirectory && cfg.stateDirectory != "/";
        message = "Executor source and release state require absolute paths.";
      }
      {
        assertion = config.services.homelabPlatform.securityClientSocket != null;
        message = "Platform must use independent authorization before enabling the executor.";
      }
      {
        assertion = config.services.homelabPlatform.executorSocket == "/run/homelab-executord/execute.sock";
        message = "Platform executor socket must match the protected local executor endpoint.";
      }
    ];
    users.groups.homelab-executor-client = {};
    environment.systemPackages = [ cfg.package (pkgs.writeShellScriptBin "homelab-releasectl" ''
      export HOMELAB_RELEASE_HOST=${lib.escapeShellArg config.networking.hostName}
      export HOMELAB_RELEASE_SOURCE=${lib.escapeShellArg cfg.sourceDirectory}
      export HOMELAB_RELEASE_STATE=${lib.escapeShellArg cfg.stateDirectory}
      export HOMELAB_RELEASE_NIX=${lib.escapeShellArg ("${pkgs.nix}/bin/nix")}
      export HOMELAB_RELEASE_NIX_ENV=${lib.escapeShellArg ("${pkgs.nix}/bin/nix-env")}
      export HOMELAB_RELEASE_GIT=${lib.escapeShellArg ("${pkgs.git}/bin/git")}
      export HOMELAB_RELEASE_SYSTEMCTL=${lib.escapeShellArg ("${pkgs.systemd}/bin/systemctl")}
      export HOMELAB_RELEASE_HEALTH_FILE=${lib.escapeShellArg (pkgs.writeText "homelab-release-health.json" (builtins.toJSON cfg.healthServices))}
      exec ${cfg.package}/bin/homelab-release "$@"
    '') ];
    systemd.services.homelab-executord = {
      description = "Restricted local infrastructure executor";
      wantedBy = [ "multi-user.target" ];
      requires = [ "homelab-securityd.service" ];
      after = [ "homelab-securityd.service" ];
      environment = {
        HOMELAB_EXECUTOR_HOST = config.networking.hostName;
        HOMELAB_EXECUTOR_RELEASE_ENABLE = if cfg.allowReleaseActivation then "true" else "false";
        HOMELAB_EXECUTOR_SECURITY_SOCKET = "/run/homelab-securityd/client.sock";
        HOMELAB_EXECUTOR_SOURCE = cfg.sourceDirectory;
        HOMELAB_EXECUTOR_RELEASE_STATE = cfg.stateDirectory;
        HOMELAB_EXECUTOR_NIX = "${pkgs.nix}/bin/nix";
        HOMELAB_EXECUTOR_NIX_ENV = "${pkgs.nix}/bin/nix-env";
        HOMELAB_EXECUTOR_GIT = "${pkgs.git}/bin/git";
        HOMELAB_EXECUTOR_SYSTEMCTL = "${pkgs.systemd}/bin/systemctl";
        HOMELAB_EXECUTOR_SERVICES_FILE = pkgs.writeText "homelab-executor-services.json" (builtins.toJSON cfg.allowedServices);
        HOMELAB_EXECUTOR_HEALTH_FILE = pkgs.writeText "homelab-executor-health.json" (builtins.toJSON cfg.healthServices);
      };
      serviceConfig = {
        Type = "simple";
        User = "root";
        Group = "root";
        SupplementaryGroups = [ "homelab-security-client" ];
        StateDirectory = "homelab-releases";
        StateDirectoryMode = "0700";
        RuntimeDirectory = "homelab-executord";
        RuntimeDirectoryMode = "0750";
        ExecStart = "${cfg.package}/bin/homelab-executord";
        Restart = "on-failure";
        RestartSec = "5s";
        UMask = "0077";
        NoNewPrivileges = true;
        PrivateNetwork = true;
        PrivateTmp = true;
        ProtectSystem = if cfg.allowReleaseActivation then false else "strict";
        ReadWritePaths = [ "/nix/var/nix/profiles" cfg.stateDirectory ];
        ProtectHome = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        CapabilityBoundingSet = "";
        RestrictAddressFamilies = [ "AF_UNIX" ];
        RestrictNamespaces = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
      };
    };
  };
}
