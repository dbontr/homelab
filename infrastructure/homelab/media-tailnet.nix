{ pkgs, ... }:
let
  mediaTailscale = pkgs.runCommand "tailscale-media-1.104.4" {
    src = pkgs.fetchurl {
      url = "https://pkgs.tailscale.com/stable/tailscale_1.104.4_amd64.tgz";
      hash = "sha256-Yk6gk9R9Ys9W8XuDMtdLjezaSh+qYQ68YKHeM0srPck=";
    };
  } ''
    mkdir -p "$out/bin"
    tar -xzf "$src" --strip-components=1
    install -m755 tailscaled "$out/bin/tailscaled"
    install -m755 tailscale "$out/bin/tailscale"
  '';
in
{
  # Userspace networking isolates media ingress from Saturn's router.
  # The state directory also holds the authenticated tailnet device identity
  # and the private Serve configuration; it must be backed up securely.
  systemd.services.saturn-media-tailnet = {
    description = "Tailnet-only media ingress";
    wantedBy = [ "multi-user.target" ];
    wants = [ "network-online.target" ];
    after = [ "network-online.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = "${mediaTailscale}/bin/tailscaled --tun=userspace-networking --statedir=/var/lib/tailscale-media --socket=/run/tailscale-media/tailscaled.sock --port=0 --no-logs-no-support";
      StateDirectory = "tailscale-media";
      RuntimeDirectory = "tailscale-media";
      LogsDirectory = "tailscale-media";
      Restart = "on-failure";
      RestartSec = "5s";
      NoNewPrivileges = true;
      ProtectSystem = "strict";
      ProtectHome = true;
      ProtectKernelTunables = true;
      ProtectControlGroups = true;
      RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" "AF_NETLINK" ];
      CapabilityBoundingSet = "";
      UMask = "0077";
    };
  };
}
