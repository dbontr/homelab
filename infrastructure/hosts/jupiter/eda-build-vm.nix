{ config, lib, pkgs, ... }:
let
  cfg = config.homelab.edaBuildVm;
  state = "/var/lib/eda-build-vm";
  vmControl = pkgs.writeShellScript "eda-vm-control" ''
    set -eu
    case "$SSH_ORIGINAL_COMMAND" in
      start)
        ${pkgs.systemd}/bin/systemctl start eda-build-vm.service
        ${pkgs.systemd}/bin/systemctl is-active eda-build-vm.service
        ;;
      status)
        ${pkgs.systemd}/bin/systemctl is-active eda-build-vm.service
        ;;
      *)
        echo "Unsupported EDA VM operation" >&2
        exit 1
        ;;
    esac
  '';
in
{
  options.homelab.edaBuildVm = {
    enable = lib.mkEnableOption
      "on-demand Ubuntu image-building VM on an approved compute node";
    controlPublicKey = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Dedicated public key with start/status-only host commands";
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.root.openssh.authorizedKeys.keys =
      lib.optional (cfg.controlPublicKey != null)
        ''restrict,command="${vmControl}" ${cfg.controlPublicKey}'';

    users.groups.eda-vm = {};
    users.users.eda-vm = {
      isSystemUser = true;
      group = "eda-vm";
      extraGroups = [ "kvm" ];
    };

    environment.systemPackages = with pkgs; [
      qemu curl openssh cdrkit
    ];

    systemd.tmpfiles.rules = [
      "d ${state} 0700 eda-vm eda-vm -"
    ];

    # This service is deliberately NOT wanted by multi-user.target. A heavy
    # image build must be explicitly reserved and started on a compute node.
    systemd.services.eda-build-vm = {
      description = "Isolated Ubuntu build VM for licensed embedded development";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      unitConfig.ConditionPathExists = "${state}/disk.qcow2";
      serviceConfig = {
        Type = "simple";
        User = "eda-vm";
        Group = "eda-vm";
        SupplementaryGroups = [ "kvm" ];
        ExecStart = lib.concatStringsSep " " [
          "${pkgs.qemu}/bin/qemu-system-x86_64"
          "-name eda-build"
          "-machine q35,accel=kvm"
          "-cpu host"
          "-smp 8"
          "-m 20480"
          "-drive file=${state}/disk.qcow2,format=qcow2,if=virtio,cache=none"
          "-drive file=${state}/seed.iso,format=raw,media=cdrom,readonly=on"
          "-netdev user,id=buildnet,hostfwd=tcp:127.0.0.1:22022-:22,dns=1.1.1.1"
          "-device virtio-net-pci,netdev=buildnet"
          "-device virtio-rng-pci"
          "-serial file:${state}/console.log"
          "-display none"
          "-monitor none"
        ];
        Restart = "no";
        TimeoutStopSec = "180s";
        MemoryMax = "26G";
        MemorySwapMax = "0";
        CPUQuota = "800%";
        Nice = 10;
        IOWeight = 20;
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        CapabilityBoundingSet = "";
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        ReadWritePaths = [ state ];
      };
    };
  };
}
