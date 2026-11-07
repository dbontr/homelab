{ config, lib, ... }:
let
  cfg = config.homelab.router;
  internalNetworks = [ "management10" "services20" "compute30" ];
  vlan = name: id: {
    netdevConfig = { Kind = "vlan"; Name = name; };
    vlanConfig.Id = id;
  };
  network = name: address: {
    matchConfig.Name = name;
    address = [ address ];
    networkConfig = {
      DHCP = "no";
      LinkLocalAddressing = "no";
      IPv6AcceptRA = false;
    };
  };
in {
  options.homelab.router = {
    enable = lib.mkEnableOption "portable NixOS WAN router and private VLANs";
    isolationVerified = lib.mkEnableOption "physical separation of private trunk and upstream LAN";
    wanInterface = lib.mkOption {
      type = lib.types.str;
      default = "enp3s0";
    };
    lanInterface = lib.mkOption {
      type = lib.types.str;
      default = "enp0s31f6";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.isolationVerified;
        message = "Saturn router cannot start before the private switch is physically isolated.";
      }
      {
        assertion = cfg.wanInterface != cfg.lanInterface;
        message = "The WAN and LAN must use different physical interfaces.";
      }
    ];

    networking.useDHCP = lib.mkForce false;
    networking.networkmanager.enable = lib.mkForce false;
    networking.useNetworkd = true;
    systemd.network.enable = true;

    systemd.network.netdevs = {
      "10-management" = vlan "management10" 10;
      "20-services" = vlan "services20" 20;
      "30-compute" = vlan "compute30" 30;
    };

    systemd.network.networks = {
      "10-wan" = {
        matchConfig.Name = cfg.wanInterface;
        networkConfig = { DHCP = "ipv4"; IPv6AcceptRA = false; };
        dhcpV4Config.UseDNS = true;
      };
      "20-lan-trunk" = {
        matchConfig.Name = cfg.lanInterface;
        vlan = internalNetworks;
        networkConfig = {
          DHCP = "no";
          LinkLocalAddressing = "no";
          IPv6AcceptRA = false;
        };
      };
      "30-management" = network "management10" "10.77.10.1/24";
      "40-services" = network "services20" "10.77.20.1/24";
      "50-compute" = network "compute30" "10.77.30.1/24";
    };

    networking.nat = {
      enable = true;
      externalInterface = cfg.wanInterface;
      internalInterfaces = internalNetworks;
    };
    boot.kernel.sysctl."net.ipv4.ip_forward" = 1;
    boot.kernel.sysctl."net.ipv6.conf.all.forwarding" = 0;

    services.dnsmasq = {
      enable = true;
      settings = {
        port = 53;
        "bind-dynamic" = true;
        interface = internalNetworks;
        "no-resolv" = true;
        server = [ "127.0.0.1#5354" ];
        "dhcp-authoritative" = true;
        "dhcp-range" = [
          "set:management,10.77.10.100,10.77.10.220,255.255.255.0,12h"
          "set:services,10.77.20.100,10.77.20.220,255.255.255.0,12h"
          "set:compute,10.77.30.100,10.77.30.220,255.255.255.0,12h"
        ];
        "dhcp-option" = [
          "tag:management,option:router,10.77.10.1"
          "tag:services,option:router,10.77.20.1"
          "tag:compute,option:router,10.77.30.1"
          "tag:management,option:dns-server,10.77.10.1"
          "tag:services,option:dns-server,10.77.20.1"
          "tag:compute,option:dns-server,10.77.30.1"
        ];
      };
    };
    networking.nftables.enable = true;
  networking.nftables.flushRuleset = false;
  networking.nftables.extraDeletions = ''
    table inet saturn_isolation;
    delete table inet saturn_isolation;
  '';
    networking.nftables.ruleset = ''
      table inet saturn_isolation {
        chain forward {
          type filter hook forward priority -10; policy accept;
          ct state established,related accept
          iifname "management10" oifname { "services20", "compute30" } accept
          iifname { "management10", "services20", "compute30" }
            oifname { "management10", "services20", "compute30" } drop
          iifname "${cfg.wanInterface}"
            oifname { "management10", "services20", "compute30" } drop
        }
      }
    '';

    services.openssh.openFirewall = lib.mkForce false;
    networking.firewall = {
      enable = true;
      allowedTCPPorts = lib.mkForce [ ];
      allowedUDPPorts = lib.mkForce [ ];
      interfaces = {
        management10.allowedTCPPorts = [ 22 53 6443 ];
        management10.allowedUDPPorts = [ 53 67 ];
        services20.allowedTCPPorts = [ 53 ];
        services20.allowedUDPPorts = [ 53 67 ];
        compute30.allowedTCPPorts = [ 53 ];
        compute30.allowedUDPPorts = [ 53 67 ];
      };
    };
  };
}
