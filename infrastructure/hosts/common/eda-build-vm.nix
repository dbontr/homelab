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
    cpuCores = lib.mkOption {
      type = lib.types.ints.between 1 64;
      default = 8;
      description = "Virtual CPUs reserved when the on-demand guest is running";
    };
    memoryMiB = lib.mkOption {
      type = lib.types.ints.between 2048 262144;
      default = 20480;
      description = "Guest memory reservation in MiB";
    };
    memoryOverheadMiB = lib.mkOption {
      type = lib.types.ints.between 1024 16384;
      default = 6144;
      description = "Additional cgroup memory allowance for QEMU and guests";
    };
    hostSshPort = lib.mkOption {
      type = lib.types.port;
      default = 22022;
      description = "Host loopback-only forwarded port for guest SSH";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.memoryMiB >= 2048;
        message = "The EDA guest requires at least 2 GiB of memory.";
      }
    ];

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

    # A compute host exposes no guest SSH listener on the LAN.
    # The VM remains stopped unless an authorized controller starts it.
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
          "-smp ${toString cfg.cpuCores}"
          "-m ${toString cfg.memoryMiB}"
          "-drive file=${state}/disk.qcow2,format=qcow2,if=virtio,cache=none"
          "-drive file=${state}/seed.iso,format=raw,media=cdrom,readonly=on"
          "-netdev user,id=buildnet,hostfwd=tcp:127.0.0.1:${toString cfg.hostSshPort}-:22,dns=1.1.1.1"
          "-device virtio-net-pci,netdev=buildnet"
          "-device virtio-rng-pci"
          "-serial file:${state}/console.log"
          "-display none"
          "-monitor none"
        ];
        Restart = "no";
        TimeoutStopSec = "180s";
        MemoryMax = "${toString (cfg.memoryMiB + cfg.memoryOverheadMiB)}M";
        MemorySwapMax = "0";
        CPUQuota = "${toString (cfg.cpuCores * 100)}%";
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
        # QEMU user networking requires public HTTPS but not LAN management access.
        IPAddressDeny = [ "10.0.0.0/8" "172.16.0.0/12"
                          "192.168.0.0/16" "169.254.0.0/16" ];
        ReadWritePaths = [ state ];
      };
    };
  };
}
