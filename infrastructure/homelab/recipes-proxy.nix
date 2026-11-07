{ ... }:
{
  # Preserve the collector and expose only data, never an alternate UI.
  services.nginx.virtualHosts."recipes.home.arpa" = {
    listen = [{ addr = "10.77.20.1"; port = 80; }];
    locations."/" = {
      proxyPass = "http://127.0.0.1:7270";
      proxyWebsockets = true;
    };
    extraConfig = "client_max_body_size 32m;";
  };

  # The Cloudflare tunnel authenticates requests before these local origins.
  services.nginx.virtualHosts."recipes.dbontr.me" = {
    listen = [{ addr = "127.0.0.1"; port = 8099; }];
    locations."/" = {
      proxyPass = "http://127.0.0.1:7270";
      proxyWebsockets = true;
      extraConfig = ''
        proxy_redirect http://recipes.dbontr.me/ https://recipes.dbontr.me/;
        proxy_redirect http://recipes.home.arpa/ https://recipes.dbontr.me/;
      '';
    };
    extraConfig = "client_max_body_size 32m;";
  };

}
