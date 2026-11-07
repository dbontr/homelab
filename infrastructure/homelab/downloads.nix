{ config, lib, pkgs, ... }:
let
  cfg = config.homelab.native.downloads;
  ns = "download-vpn";
  confinedServices = [ "qbittorrent" "prowlarr" "radarr" "sonarr" "lidarr" ];
  confinedServiceConfig = {
    NetworkNamespacePath = "/run/netns/${ns}";
    # systemd does not automatically apply /etc/netns/<name> files, and a host
    # nscd socket can resolve names outside the selected network namespace.
    BindReadOnlyPaths = [
      "/etc/netns/${ns}/resolv.conf:/etc/resolv.conf:norbind"
      "/etc/netns/${ns}/nsswitch.conf:/etc/nsswitch.conf:norbind"
    ];
    TemporaryFileSystem = [ "/run/nscd" "/run/systemd/resolve" ];
  };
  confinedUnits = lib.genAttrs confinedServices (name:
    let
      mediaRequired = builtins.elem name [ "qbittorrent" "radarr" "sonarr" "lidarr" ];
      storageDependency = lib.optional mediaRequired "saturn-download-media-permissions.service";
    in {
      requires = [ "saturn-download-vpn.service" "saturn-download-dns.service" ] ++ storageDependency;
      after = [ "saturn-download-vpn.service" "saturn-download-dns.service" ] ++ storageDependency;
      serviceConfig = confinedServiceConfig;
    });
  # Seerr can reach only the two authenticated Arr APIs. Socket descriptors
  # originate in the media bridge; upstream connects stay inside the VPN netns.
  seerrBackends = {
    radarr = { listenPort = 17878; targetPort = 7878; };
    sonarr = { listenPort = 18989; targetPort = 8989; };
  };
  seerrSockets = lib.mapAttrs' (name: backend:
    lib.nameValuePair "saturn-seerr-${name}" {
      wantedBy = [ "sockets.target" ];
      socketConfig = {
        ListenStream = "172.19.0.1:${toString backend.listenPort}";
        # Socket activation starts before Docker creates the media gateway.
        FreeBind = true;
        Accept = false;
      };
    }) seerrBackends;
  seerrServices = lib.mapAttrs' (name: backend:
    lib.nameValuePair "saturn-seerr-${name}" {
      description = "Private Seerr bridge into VPN-confined ${name}";
      requires = [ "${name}.service" "saturn-download-vpn.service" ];
      after = [ "${name}.service" "saturn-download-vpn.service" ];
      serviceConfig = {
        Type = "notify";
        ExecStart = "${pkgs.systemd}/lib/systemd/systemd-socket-proxyd --exit-idle-time=5min 127.0.0.1:${toString backend.targetPort}";
        NetworkNamespacePath = "/run/netns/${ns}";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
      };
    }) seerrBackends;
in {
  options.homelab.native.downloads = {
    enable = lib.mkEnableOption "VPN-confined native download applications";
    readarr.enable = lib.mkEnableOption "archived Readarr service";
  };

  config = lib.mkIf cfg.enable {
    environment.systemPackages = [ pkgs.wireguard-tools pkgs.iproute2 ];
    # Restrict host-side Arr bridge access to the Seerr container on the
    # private media network. These rules do not expose an upstream/WAN port.
    networking.firewall.extraInputRules = ''
      iifname "br-*" ip saddr 172.19.0.3 ip daddr 172.19.0.1 tcp dport { 17878, 18989 } accept
    '';
    systemd.sockets = seerrSockets;
    environment.etc."netns/${ns}/resolv.conf".text = ''
      nameserver 127.0.0.53
      options timeout:2 attempts:2
    '';
    environment.etc."netns/${ns}/nsswitch.conf".text = ''
      passwd: files
      group: files
      shadow: files
      hosts: files dns
      networks: files
      services: files
      protocols: files
      rpc: files
      ethers: files
    '';

    systemd.services = confinedUnits // seerrServices // {
    saturn-download-dns = {
      description = "DNS resolver confined to Surfshark";
      requires = [ "saturn-download-vpn.service" ];
      after = [ "saturn-download-vpn.service" ];
      serviceConfig = {
        Type = "simple";
        ExecStart = "${pkgs.dnsmasq}/bin/dnsmasq --conf-file=/dev/null --no-daemon --no-resolv --no-hosts --bind-interfaces --listen-address=127.0.0.53 --server=162.252.172.57 --server=149.154.159.92 --port=53";
        NetworkNamespacePath = "/run/netns/${ns}";
        Restart = "on-failure";
        RestartSec = "5s";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        PrivateTmp = true;
      };
    };
      saturn-download-vpn = {
        description = "Isolated Surfshark WireGuard network namespace";
        requiredBy = [ "multi-user.target" ];
        before = map (name: "${name}.service") confinedServices;
        unitConfig.AssertPathExists = [
          "/srv/homelab/secrets/surfshark-wg.conf"
          "/srv/homelab/secrets/surfshark-address"
        ];
        serviceConfig = {
          Type = "oneshot";
          RemainAfterExit = true;
          TimeoutStartSec = "25s";
          ExecReload = "${pkgs.writeShellScript "restore-download-vpn-route" ''
            set -euo pipefail
            ${pkgs.iproute2}/bin/ip -n ${ns} link set wg-download up
            ${pkgs.iproute2}/bin/ip -n ${ns} route replace default dev wg-download
          ''}";
        };
        script = ''
          set -euo pipefail
          ip="${pkgs.iproute2}/bin/ip"
          wg="${pkgs.wireguard-tools}/bin/wg"
          $ip netns add ${ns}
          $ip -n ${ns} link set lo up
          $ip link add wg-download type wireguard
          $ip link set wg-download netns ${ns}
          $ip netns exec ${ns} $wg setconf wg-download /srv/homelab/secrets/surfshark-wg.conf
          $ip -n ${ns} address add "$(${pkgs.coreutils}/bin/cat /srv/homelab/secrets/surfshark-address)" dev wg-download
          $ip -n ${ns} link set wg-download up
          $ip -n ${ns} route add default dev wg-download
        '';
        postStop = ''
          ${pkgs.iproute2}/bin/ip netns delete ${ns} || true
        '';
      };
    } // lib.optionalAttrs cfg.readarr.enable {
      readarr = {
        requires = [ "saturn-download-vpn.service" "saturn-download-dns.service" ];
        after = [ "saturn-download-vpn.service" "saturn-download-dns.service" ];
        serviceConfig = confinedServiceConfig;
      };
    };

    services.qbittorrent = {
      enable = true;
      openFirewall = false;
      webuiPort = 8080;
      profileDir = "/var/lib/qbittorrent";
    };
    services.prowlarr.enable = true;
    services.radarr.enable = true;
    services.sonarr.enable = true;
    services.lidarr.enable = true;
    services.readarr.enable = cfg.readarr.enable;
  };
}
