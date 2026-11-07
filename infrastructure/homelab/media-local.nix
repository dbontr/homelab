{ pkgs, ... }:
let
  conf = pkgs.writeText "saturn-media-local-nginx.conf" (builtins.readFile ./media-local-nginx.conf);
in {
  services.dnsmasq.settings.address = [ "/media.tail305103.ts.net/10.77.20.1" ];
  services.dnsmasq.settings.local = [ "/media.tail305103.ts.net/" ];
  networking.firewall.interfaces.services20.allowedTCPPorts = [ 443 ];
  networking.firewall.interfaces.management10.allowedTCPPorts = [ 443 ];
  networking.firewall.interfaces.compute30.allowedTCPPorts = [ 443 ];
  systemd.services.saturn-media-local = {
    description = "Moonfin local HTTPS ingress";
    wantedBy = [ "multi-user.target" ];
    after = [ "network-online.target" "saturn-media.service" "saturn-media-tailnet.service" ];
    wants = [ "network-online.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = "${pkgs.nginx}/bin/nginx -c ${conf} -g 'daemon off;'";
      Restart = "on-failure";
      RestartSec = "10s";
      StateDirectory = "saturn-media-local-tls";
      StateDirectoryMode = "0700";
      ExecStartPre = "${pkgs.tailscale}/bin/tailscale --socket=/run/tailscale-media/tailscaled.sock cert --cert-file=/var/lib/saturn-media-local-tls/media.crt --key-file=/var/lib/saturn-media-local-tls/media.key media.tail305103.ts.net";
    };
  };
  systemd.services.saturn-media-local-cert = {
    description = "Renew Moonfin HTTPS certificate";
    serviceConfig = {
      Type = "oneshot";
      ExecStart = "${pkgs.tailscale}/bin/tailscale --socket=/run/tailscale-media/tailscaled.sock cert --cert-file=/var/lib/saturn-media-local-tls/media.crt --key-file=/var/lib/saturn-media-local-tls/media.key media.tail305103.ts.net";
      ExecStartPost = "-${pkgs.nginx}/bin/nginx -s reload -c ${conf}";
    };
  };
  systemd.timers.saturn-media-local-cert = {
    wantedBy = [ "timers.target" ];
    timerConfig = { OnCalendar = "weekly"; Persistent = true; };
  };
}
