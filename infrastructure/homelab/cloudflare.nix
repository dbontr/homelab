{ config, lib, pkgs, ... }:
let
  credentialsPath = "/srv/homelab/secrets/cloudflared-saturn.json";
  homepageUnit = if config.services.homelabConsole.enable && config.services.homelabConsole.replaceHomepage
    then "nginx.service" else "homepage-dashboard.service";
  configFile = (pkgs.formats.yaml { }).generate "saturn-cloudflared.yml" {
    tunnel = "132a5be7-22b0-4697-a46a-4683bcee9a25";
    "credentials-file" = "/run/credentials/cloudflared-saturn.service/tunnel.json";
    ingress = [
      {
        hostname = "dbontr.me";
        service = "http://127.0.0.1:8082";
        originRequest.access = {
          required = true;
          teamName = "dbontr";
          audTag = [ config.homelab.access.consoleAudTag ];
        };
      }
      {
        hostname = "media.dbontr.me";
        service = "http://127.0.0.1:8097";
        originRequest.access = {
          required = true;
          teamName = "dbontr";
          audTag = [ config.homelab.access.mediaAudTag ];
        };
      }
      {
        hostname = "recipes.dbontr.me";
        service = "http://127.0.0.1:8099";
        originRequest.access = {
          required = true;
          teamName = "dbontr";
          audTag = [ config.homelab.access.recipesAudTag ];
        };
      }
      {
        hostname = "mcp.dbontr.me";
        service = "http://127.0.0.1:8104";
        originRequest.access = {
          required = true;
          teamName = "dbontr";
          audTag = [ "85181fd33a10983cc64020e9155ffeeea8f9e4c6b7af6dd1a51f5c3588f63f25" ];
        };
      }
    ] ++
      lib.optional config.services.homelabSecurity.browser.enable {
        hostname = config.services.homelabSecurity.browser.hostname;
        service = "http://127.0.0.1:8115";
        originRequest.access = {
          required = true;
          teamName = "dbontr";
          audTag = [ config.services.homelabSecurity.browser.audience ];
        };
      } ++
      lib.optional (config.homelab.access.monitorAudTag != null) {
        hostname = "monitor.dbontr.me";
        service = "http://127.0.0.1:3000";
        originRequest.access = {
          required = true;
          teamName = "dbontr";
          audTag = [ config.homelab.access.monitorAudTag ];
        };
      } ++
      [ { service = "http_status:404"; } ];
  };
in
{
  options.homelab.access = {
    consoleAudTag = lib.mkOption {
      type = lib.types.str;
      default = "f409f391d98b48008017a2ab9b4d18c4800ed412b2ef5f7b131742e2864e62b1";
      description = "Cloudflare Access application audience for dbontr.me.";
    };
    mediaAudTag = lib.mkOption {
      type = lib.types.str;
      default = "6440ec8c06ff6d3644330d172eef02b02af55a55fda035494f2939d9bdc593b6";
      description = "Cloudflare Access audience for the household media application.";
    };
    recipesAudTag = lib.mkOption {
      type = lib.types.str;
      default = "6440ec8c06ff6d3644330d172eef02b02af55a55fda035494f2939d9bdc593b6";
      description = "Cloudflare Access audience for the household recipe application.";
    };
    monitorAudTag = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Audience for the owner-only Cloudflare Access application on monitor.dbontr.me. Null does not publish this hostname.";
    };
  };
  config.systemd.services.cloudflared-saturn = {
    description = "Private Cloudflare Tunnel for dbontr.me";
    wantedBy = [ "multi-user.target" ];
    after = [ "network-online.target" homepageUnit ];
    wants = [ "network-online.target" homepageUnit ];
    unitConfig.ConditionFileNotEmpty = credentialsPath;
    serviceConfig = {
      Type = "simple";
      ExecStart = "${pkgs.cloudflared}/bin/cloudflared tunnel --config=${configFile} --no-autoupdate run";
      LoadCredential = [ "tunnel.json:${credentialsPath}" ];
      Restart = "on-failure";
      RestartSec = "10s";
      DynamicUser = true;
      NoNewPrivileges = true;
      PrivateTmp = true;
      ProtectSystem = "strict";
      ProtectHome = true;
      CapabilityBoundingSet = "";
      RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
      UMask = "0077";
    };
  };
}
