{ ... }:
{
  # Nginx starts only after the services VLAN is configured.
  systemd.services.nginx = {
    wants = [ "network-online.target" ];
    after = [ "network-online.target" ];
  };

  systemd.tmpfiles.rules = [
    "f /var/log/nginx/error.log 0640 nginx nginx -"
  ];

  # Retain both trusted hostnames but direct browsers to the owner-authenticated
  # first-party Recipes page. The Cloudflare tunnel keeps its Access policy.
  services.nginx.virtualHosts."recipes.home.arpa" = {
    listen = [{ addr = "10.77.20.1"; port = 80; }];
    locations."/" = {
      extraConfig = ''
        add_header Cache-Control "no-store" always;
        return 302 https://dbontr.me/recipes;
      '';
    };
  };

  services.nginx.virtualHosts."recipes.dbontr.me" = {
    listen = [{ addr = "127.0.0.1"; port = 8099; }];
    locations."/" = {
      extraConfig = ''
        add_header Cache-Control "no-store" always;
        return 302 https://dbontr.me/recipes;
      '';
    };
  };
}
