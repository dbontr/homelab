{ pkgs, ... }:
{
  systemd.tmpfiles.rules = [
    "d /run/homelab-gpu 0755 root root - -"
  ];

  systemd.services.saturn-gpu-textfile = {
    description = "Publish restricted GPU telemetry for Prometheus";
    after = [ "k3s.service" ];
    wants = [ "k3s.service" ];
    path = with pkgs; [ bash coreutils jq k3s ];
    serviceConfig = {
      Type = "oneshot";
      ExecStart = "${pkgs.bash}/bin/bash ${./gpu-telemetry.sh}";
      User = "root";
      Group = "root";
      NoNewPrivileges = true;
      PrivateTmp = true;
      ProtectHome = true;
      ProtectSystem = "strict";
      ReadWritePaths = [ "/run/homelab-gpu" ];
      UMask = "0022";
      TimeoutStartSec = "30s";
    };
  };

  systemd.timers.saturn-gpu-textfile = {
    description = "Refresh Prometheus GPU textfile";
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnBootSec = "35s";
      OnUnitActiveSec = "45s";
      AccuracySec = "10s";
    };
  };
}
