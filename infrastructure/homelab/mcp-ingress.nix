{ ... }:
let
  streamProxy = ''
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_read_timeout 300s;
  '';
  workspaceProxy = destination: streamProxy + ''
    set $workspace_host pwm-controller.pwm-system.svc.cluster.local;
    proxy_pass https://$workspace_host:8443/${destination};
    proxy_set_header Host pwm-controller.pwm-system.svc.cluster.local;
    proxy_ssl_name pwm-controller.pwm-system.svc.cluster.local;
    proxy_ssl_server_name on;
    proxy_ssl_verify on;
    proxy_ssl_verify_depth 2;
    proxy_ssl_trusted_certificate /srv/mcp-trust/pwm-controller-ca.pem;
  '';
in
{
  # Access authenticates the shared origin; upstream services enforce their own authorization.
  services.nginx.virtualHosts."mcp.dbontr.me" = {
    listen = [{ addr = "127.0.0.1"; port = 8104; }];
    extraConfig = ''
      resolver 10.43.0.10 valid=30s ipv6=off;
      client_max_body_size 64m;
    '';

    locations."= /workspace".extraConfig = workspaceProxy "mcp";
    locations."= /adaptive-teaching".extraConfig = streamProxy + ''
      proxy_pass http://127.0.0.1:8200/mcp;
      proxy_set_header Host 127.0.0.1:8200;
    '';
    locations."= /personal-health".extraConfig = streamProxy + ''
      proxy_pass http://127.0.0.1:8201/mcp;
      proxy_set_header Host 127.0.0.1:8201;
    '';
    locations."/".return = "404";
  };
}
