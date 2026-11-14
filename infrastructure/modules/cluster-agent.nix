{ config, lib, ... }:
let
  cfg = config.homelab.clusterAgent;
in {
  options.homelab.clusterAgent = {
    enable = lib.mkEnableOption "NixOS K3s compute agent";
    controlPlane = lib.mkOption {
      type = lib.types.str;
      default = "10.77.30.1";
      description = "Saturn K3s API address reachable on the compute VLAN.";
    };
    nodeIP = lib.mkOption {
      type = lib.types.str;
      description = "Stable IPv4 address assigned to this compute node.";
    };
    lanInterface = lib.mkOption {
      type = lib.types.str;
      description = "Agent NIC connected to Saturn's cluster LAN.";
    };
    tokenFile = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/secrets/k3s-agent-token";
      description = "Runtime path for the agent join token, outside the Nix store.";
    };
  };

  config = lib.mkIf cfg.enable {
    services.k3s = {
      enable = true;
      role = "agent";
      serverAddr = "https://${cfg.controlPlane}:6443";
      tokenFile = cfg.tokenFile;
      nodeIP = cfg.nodeIP;
      extraFlags = [
        "--node-label=homelab-role=compute"
        "--flannel-iface=${cfg.lanInterface}"
      ];
    };
    # K3s Spegel shares locally imported images only for explicitly mirrored registries.
    environment.etc."rancher/k3s/registries.yaml".text = ''
      mirrors:
        "images.dbontr.internal":
        "ghcr.io":
    '';
    services.prometheus.exporters.node = {
      enable = true;
      port = 9100;
      openFirewall = false;
    };
    networking.firewall.interfaces.${cfg.lanInterface} = {
      allowedTCPPorts = [ 5001 6443 10250 9100 ];
      allowedUDPPorts = [ 8472 ];
    };
    assertions = [
      { assertion = config.networking.hostName != null
          && config.networking.hostName != ""
          && config.networking.hostName != "saturn";
        message = "K3s agents must use a unique non-Saturn hostname."; }
      { assertion = cfg.nodeIP != cfg.controlPlane;
        message = "A K3s agent cannot use Saturn's control-plane IP."; }
      { assertion = !config.networking.nat.enable;
        message = "Compute nodes must not provide upstream NAT."; }
      { assertion = !config.services.dnsmasq.enable;
        message = "Compute nodes must not run a DHCP/DNS router."; }
    ];
  };
}
