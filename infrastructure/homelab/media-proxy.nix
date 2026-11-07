{ ... }:
{
  # The Moonbase web application is the media landing page; keep Jellyfin APIs reachable.
  services.nginx.virtualHosts."moonfin.local" = {
    locations."= /Moonfin/Web/config.json" = {
      proxyPass = "http://127.0.0.1:8096";
      extraConfig = ''
        proxy_set_header Accept-Encoding "";
        proxy_hide_header ETag;
        add_header Cache-Control "no-store" always;
        sub_filter_types application/json;
        sub_filter_once off;
        sub_filter 'http://127.0.0.1' 'https://media.tail305103.ts.net';
        sub_filter '"enableWebRtcScan":true' '"enableWebRtcScan":false';
      '';
    };
    locations."~ ^/Moonfin/Discovery(/discover)?$" = {
      proxyPass = "http://127.0.0.1:8096";
      extraConfig = ''
        proxy_set_header Accept-Encoding "";
        proxy_hide_header ETag;
        add_header Cache-Control "no-store" always;
        sub_filter_types application/json;
        sub_filter_once off;
        sub_filter 'http://127.0.0.1' 'https://media.tail305103.ts.net';
      '';
    };
    locations."/Moonfin/assets/" = {
      proxyPass = "http://127.0.0.1:8096/Moonfin/Web/assets/";
    };

    listen = [{ addr = "127.0.0.1"; port = 8097; }];
    extraConfig = "absolute_redirect off;";
    locations."= /".return = "302 /Moonfin/Web/";
    locations."= /Moonfin/Web/flutter_bootstrap.js" = {
      proxyPass = "http://127.0.0.1:8096";
      extraConfig = ''
        proxy_hide_header Cache-Control;
        add_header Cache-Control "no-store" always;
      '';
    };
    locations."/" = {
      proxyPass = "http://127.0.0.1:8096";
      proxyWebsockets = true;
    };
  };

}
