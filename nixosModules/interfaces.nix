{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabInterfaces;
  package = import ../packages/interfaces.nix { inherit pkgs; };
  upstream = "http://" + config.services.homelabPlatform.listenAddress;
  common = kind: settings: {
    description = "Homelab optional " + kind + " interface";
    wantedBy = [ "multi-user.target" ];
    wants = [ "homelab-platform.service" ];
    after = [ "network.target" "homelab-platform.service" ];
    environment = {
      "HOMELAB_${lib.toUpper kind}_LISTEN_ADDR" = settings.listenAddress;
      "HOMELAB_${lib.toUpper kind}_BACKEND_URL" = upstream;
      "HOMELAB_${lib.toUpper kind}_CLIENT_TOKEN_FILE" =
        "/run/credentials/homelab-${kind}.service/client-token";
      "HOMELAB_${lib.toUpper kind}_SERVICE_TOKEN_FILE" =
        "/run/credentials/homelab-${kind}.service/service-token";
      "HOMELAB_${lib.toUpper kind}_ALLOWED_ORIGINS" =
        lib.concatStringsSep "," settings.allowedOrigins;
    } // lib.optionalAttrs (kind == "api") {
      HOMELAB_API_WRITE_PATHS = lib.concatStringsSep "," settings.writePaths;
    };
    serviceConfig = {
      Type = "simple";
      DynamicUser = true;
      ExecStart = "${package}/bin/homelab-${kind}";
      LoadCredential = [
        "client-token:${settings.clientTokenFile}"
        "service-token:${settings.serviceTokenFile}"
      ];
      Restart = "on-failure";
      RestartSec = "5s";
      UMask = "0077";
      NoNewPrivileges = true;
      PrivateTmp = true;
      PrivateDevices = true;
      ProtectHome = true;
      ProtectSystem = "strict";
      ProtectKernelTunables = true;
      ProtectKernelModules = true;
      ProtectControlGroups = true;
      CapabilityBoundingSet = "";
      RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
      IPAddressDeny = "any";
      IPAddressAllow = [ "localhost" ];
    };
  };
  interfaceOptions = {
    enable = lib.mkEnableOption "independent interface listener";
    listenAddress = lib.mkOption {
      type = lib.types.str;
      description = "Exact loopback listener address, separate from the service runtime.";
    };
    clientTokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected client-facing credential, distinct from the service runtime token.";
    };
    serviceTokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Dedicated private service identity token; never reuse a platform administrator token.";
    };
    allowedOrigins = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Exact trusted browser Origin values; empty permits non-browser clients only.";
    };
    writePaths = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Explicit native resource prefixes permitted for non-read-only API methods.";
    };
  };
  secure = settings:
    let
      address = settings.listenAddress;
      port = if lib.hasPrefix "127.0.0.1:" address then
        lib.removePrefix "127.0.0.1:" address
      else if lib.hasPrefix "[::1]:" address then
        lib.removePrefix "[::1]:" address
      else "";
    in port != "" && builtins.match "[0-9]+" port != null
      && settings.clientTokenFile != null
      && settings.serviceTokenFile != null
      && settings.clientTokenFile != settings.serviceTokenFile
      && settings.serviceTokenFile != config.services.homelabPlatform.tokenFile;
in {
  options.services.homelabInterfaces = {
    mcp = lib.mkOption {
      type = lib.types.submodule { options = interfaceOptions; };
      default = { listenAddress = "127.0.0.1:8401"; };
      description = "Single aggregated MCP entry point; does not own domain state.";
    };
    api = lib.mkOption {
      type = lib.types.submodule { options = interfaceOptions; };
      default = { listenAddress = "127.0.0.1:8402"; };
      description = "Optional native HTTP API entry point.";
    };
  };
  config = {
    assertions = [
      {
        assertion = !cfg.mcp.enable || (config.services.homelabPlatform.enable && secure cfg.mcp);
        message = "MCP requires an enabled domain service and a protected, loopback-only listener.";
      }
      {
        assertion = !cfg.api.enable || (config.services.homelabPlatform.enable && secure cfg.api);
        message = "Public API requires an enabled domain service and a protected, loopback-only listener.";
      }
      {
        assertion = !(cfg.mcp.enable && cfg.api.enable) ||
          (cfg.mcp.listenAddress != cfg.api.listenAddress &&
           cfg.mcp.clientTokenFile != cfg.api.clientTokenFile &&
           cfg.mcp.serviceTokenFile != cfg.api.serviceTokenFile &&
           cfg.mcp.clientTokenFile != cfg.api.serviceTokenFile &&
           cfg.api.clientTokenFile != cfg.mcp.serviceTokenFile);
        message = "MCP and API listeners and credentials must be independent.";
      }
      {
        assertion = !cfg.mcp.enable || cfg.mcp.writePaths == [];
        message = "The unified MCP has no native API write-path configuration.";
      }
    ];
    services.homelabPlatform = lib.mkMerge [
      (lib.mkIf cfg.mcp.enable {
        mcpInterfaceTokenFile = cfg.mcp.serviceTokenFile;
      })
      (lib.mkIf cfg.api.enable {
        apiInterfaceTokenFile = cfg.api.serviceTokenFile;
        apiInterfaceWritePaths = cfg.api.writePaths;
      })
    ];
    systemd.services.homelab-mcp = lib.mkIf cfg.mcp.enable (common "mcp" cfg.mcp);
    systemd.services.homelab-api = lib.mkIf cfg.api.enable (common "api" cfg.api);
  };
}
