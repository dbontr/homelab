{ config, lib, pkgs, ... }: {
  nixpkgs.config.allowUnfree = true;
  hardware.graphics.enable = true;
  services.xserver.videoDrivers = [ "nvidia" ];
  hardware.nvidia.open = true;
  hardware.nvidia.modesetting.enable = true;
  hardware.nvidia.package = config.boot.kernelPackages.nvidiaPackages.stable;
  hardware.nvidia-container-toolkit.enable = true;
  systemd.services.k3s.path = [ (lib.getOutput "tools" config.hardware.nvidia-container-toolkit.package) ];

  services.prometheus.exporters.node.extraFlags = [
    "--collector.textfile.directory=/run/homelab-gpu"
  ];
  systemd.tmpfiles.rules = [ "d /run/homelab-gpu 0755 root root - -" ];
  systemd.services.jupiter-gpu-textfile = {
    description = "Collect Jupiter GPU metrics for node-exporter";
    path = with pkgs; [ bash coreutils ] ++ [ config.hardware.nvidia.package.bin ];
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
      TimeoutStartSec = "20s";
    };
  };
  systemd.timers.jupiter-gpu-textfile = {
    description = "Refresh Jupiter GPU metrics";
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnBootSec = "45s";
      OnUnitActiveSec = "45s";
      AccuracySec = "10s";
    };
  };
}
