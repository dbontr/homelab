{ config, lib, pkgs, ... }:
let
  cfg = config.services.personalHealthMcp.recipesage;
  compose = "${pkgs.docker-compose}/bin/docker-compose";
in {
  options.services.personalHealthMcp.recipesage = {
    enable = lib.mkEnableOption "RecipeSage application for Personal Health MCP";
    directory = lib.mkOption {
      type = lib.types.str;
      default = "/srv/personal-health/recipesage";
      description = "Directory containing the RecipeSage Compose manifests.";
    };
    environmentFile = lib.mkOption {
      type = lib.types.str;
      default = "/srv/personal-health/secrets/recipesage.env";
      description = "Owner-protected RecipeSage configuration and credentials.";
    };
  };

  config = lib.mkIf cfg.enable {
    virtualisation.docker.enable = true;
    systemd.services.recipesage = {
      description = "RecipeSage application";
      wantedBy = [ "multi-user.target" ];
      requires = [ "docker.service" ];
      after = [ "docker.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        WorkingDirectory = cfg.directory;
        ExecStart = "${compose} --env-file ${lib.escapeShellArg cfg.environmentFile} -f compose.yaml -f override.yaml up -d --no-recreate --pull never";
        ExecStop = "${compose} --env-file ${lib.escapeShellArg cfg.environmentFile} -f compose.yaml -f override.yaml stop";
        TimeoutStartSec = "15min";
        TimeoutStopSec = "3min";
      };
    };
  };
}
