{ config, lib, pkgs, ... }:
let
  cfg = config.homelab.recovery.selected;
in
{
  options.homelab.recovery.selected = {
    enable = lib.mkEnableOption "encrypted, selected-state recovery archives";

    learningDatabase = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/var/lib/adaptive-teaching/learning.db";
      description = "Absolute path to the deployed learning server's SQLite database. Null excludes learning state.";
    };

    legacyProjectKnowledge = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Explicit migration-only inclusion of legacy local knowledge until Git readback and restoration are verified. Disabled for Git-backed projects.";
    };

    remoteTarget = lib.mkOption {
      type = lib.types.str;
      default = "root@10.77.30.232";
      description = "Authorized encrypted recovery host over pinned SSH.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.saturn-selected-backup = {
      description = "Selected encrypted recovery archive and remote verification";
      wants = [ "network-online.target" "docker.service" ];
      after = [ "network-online.target" "docker.service" ];
      path = with pkgs; [ bash git docker gzip gnutar age openssh coreutils util-linux sqlite ];
      environment = {
        RECOVERY_FLAKE_REPO = "/etc/nixos";
        RECOVERY_LEARNING_DB = if cfg.learningDatabase == null then "" else cfg.learningDatabase;
        RECOVERY_LEGACY_KNOWLEDGE = if cfg.legacyProjectKnowledge then "/srv/project-knowledge" else "";
        RECOVERY_REMOTE_TARGET = cfg.remoteTarget;
      };
      serviceConfig = {
        Type = "oneshot";
        ExecStart = "${pkgs.bash}/bin/bash ${./selected-backup.sh}";
        UMask = "0077";
        TimeoutStartSec = "2h";
      };
    };

    systemd.timers.saturn-selected-backup = {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnCalendar = "daily";
        Persistent = true;
        RandomizedDelaySec = "30min";
      };
    };
  };
}
