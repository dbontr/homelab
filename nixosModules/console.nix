{ config, lib, ... }:
let
  cfg = config.services.homelabConsole;
  port = if cfg.replaceHomepage then 8082 else 8083;
  api = "http://" + config.services.homelabPlatform.listenAddress;
in
{
  options.services.homelabConsole = {
    enable = lib.mkEnableOption "the unified Rust/Dioxus console";
    package = lib.mkOption {
      type = lib.types.nullOr lib.types.package;
      default = null;
      description = "Built Dioxus web output containing index.html and the assets directory.";
    };
    replaceHomepage = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Use the console on the existing homepage origin and port instead of the legacy dashboard.";
    };
    publicOrigin = lib.mkOption {
      type = lib.types.str;
      default = "https://dbontr.me";
      description = "Exact HTTPS origin for the console when replacing the public homepage.";
    };
  };

  config = lib.mkMerge [
    {
      assertions =
        lib.optional cfg.enable {
          assertion = cfg.package != null && config.services.homelabPlatform.enable;
          message = "The console requires its built web package and an enabled Go platform API.";
        }
        ++ lib.optional (cfg.enable && cfg.replaceHomepage) {
          assertion = lib.elem cfg.publicOrigin config.services.homelabPlatform.allowedOrigins;
          message = "The console's publicOrigin must be included in homelabPlatform.allowedOrigins for browser login.";
        }
        ++ lib.optional (cfg.enable && cfg.replaceHomepage) {
          assertion =
            config.services.homelabPlatform.consolePassphraseFile != null &&
            config.services.homelabPlatform.consolePassphraseFile !=
              lib.attrByPath [ "services" "homelabSecurity" "passwordVerifierFile" ] null config;
          message = "Homepage replacement requires a dedicated browser verifier, distinct from the privileged administrator verifier.";
        };
    }
    (lib.mkIf (cfg.enable && cfg.replaceHomepage) {
      # Keep the public browser Origin enabled by default on a cutover. An
      # explicit custom allowlist overrides this default and must include it.
      services.homelabPlatform.allowedOrigins = lib.mkDefault [ cfg.publicOrigin ];
    })
    (lib.mkIf (cfg.enable && cfg.package != null) {
      services.nginx.enable = true;
      services.nginx.virtualHosts."homelab-console-loopback" = {
        listen = [{ addr = "127.0.0.1"; inherit port; ssl = false; }];
        root = "${cfg.package}";
        locations."/" = {
          tryFiles = "$uri $uri/ /index.html";
          extraConfig = ''
            add_header X-Content-Type-Options nosniff always;
            add_header Referrer-Policy no-referrer always;
          '';
        };
        locations."/v1/" = {
          proxyPass = api;
        };
        locations."/ui/" = {
          proxyPass = api;
        };
        locations."/mcp" = { return = "404"; };
      };
      services.homepage-dashboard.enable =
        lib.mkIf cfg.replaceHomepage (lib.mkForce false);
    })
  ];
}
