{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabPlatform;
  package = import ../packages/platform.nix { inherit pkgs; };
in
{
  options.services.homelabPlatform = {
    enable = lib.mkEnableOption "the integrated personal computing API and MCP gateway";
    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      description = "Go platform executable.";
    };
    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8300";
    };
    timeZone = lib.mkOption {
      type = lib.types.str;
      default = "America/Chicago";
    };
    allowedOrigins = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Exact browser or MCP Origin values permitted by the gateway; no wildcards.";
    };
    books.contactFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected contact email file used to identify on-demand Open Library search requests.";
    };
    vault = {
      endpoint = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Private mTLS credential service HTTPS origin."; };
      serverName = lib.mkOption { type = lib.types.str; default = ""; description = "Pinned vault TLS server identity."; };
      caFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Protected vault trust roots."; };
      certificateFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Protected platform client identity certificate."; };
      keyFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Protected platform client identity private key."; };
    };
    capabilityModel = {
      url = lib.mkOption { type = lib.types.str; default = ""; description = "Approved HTTPS or loopback model endpoint for read-only workflow planning."; };
      name = lib.mkOption { type = lib.types.str; default = ""; description = "Exact model name."; };
      tokenFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Owner-private provider token file."; };
      vaultAlias = lib.mkOption { type = lib.types.str; default = ""; description = "Vault alias for an allowlisted planning model credential."; };
      routes = lib.mkOption {
        type = lib.types.listOf (lib.types.submodule {
          options = {
            id = lib.mkOption { type = lib.types.str; description = "Stable route name."; };
            purpose = lib.mkOption { type = lib.types.enum [ "planning" "continuation" "evidence" ]; };
            url = lib.mkOption { type = lib.types.str; description = "Fixed HTTPS or loopback chat-completion endpoint."; };
            model = lib.mkOption { type = lib.types.str; description = "Exact selected model."; };
            localOnly = lib.mkOption { type = lib.types.bool; default = false; description = "Reject non-loopback endpoints for this purpose."; };
            tokenFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Owner-private bearer token file, loaded into this service only."; };
            vaultAlias = lib.mkOption { type = lib.types.str; default = ""; description = "Owner vault alias for this model purpose."; };
          };
        });
        default = [];
        description = "Purpose-bound, privacy-preserving model routing for read-only capability plans.";
      };
    };
    monitoring = {
      prometheusURL = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Loopback Prometheus origin for authenticated monitoring views.";
      };
      lokiURL = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Loopback Loki origin for authenticated journal inspection.";
      };
    };
    seerr = {
      url = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Private Seerr API origin.";
      };
      apiKeyFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected path containing the Seerr API key.";
      };
      vaultAlias = lib.mkOption { type = lib.types.str; default = ""; description = "Vault alias for Seerr's scoped API key."; };
    };
    devices = lib.mkOption {
      type = lib.types.listOf (lib.types.submodule {
        options = {
          id = lib.mkOption { type = lib.types.str; };
          kind = lib.mkOption { type = lib.types.enum [ "fpga" "gpu" "npu" "other" ]; };
          mode = lib.mkOption {
            type = lib.types.enum [ "ssh" "enrolled" ];
            description = "SSH is agentless; enrolled hardware can use managed workers.";
          };
          ssh = lib.mkOption {
            type = lib.types.nullOr (lib.types.submodule {
              options = {
                host = lib.mkOption { type = lib.types.str; };
                user = lib.mkOption { type = lib.types.str; };
                port = lib.mkOption { type = lib.types.port; default = 22; };
              };
            });
            default = null;
            description = "Optional unprivileged SSH login target; credentials and host keys are external.";
          };
          connection = lib.mkOption { type = lib.types.enum [ "ssh" "usb" "pcie" "local" ]; };
          node = lib.mkOption { type = lib.types.str; default = ""; };
          allowedOperations = lib.mkOption {
            type = lib.types.listOf lib.types.str;
            default = [];
          };
          eligibleForCompute = lib.mkOption {
            type = lib.types.bool;
            default = false;
          };
        };
      });
      default = (import ../hardware.nix).devices;
      description = "Declared hardware inventory. Transport does not imply connectivity or permission to modify devices.";
    };
    securityLoginURL = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Exact protected HTTPS origin where the owner signs in; the MCP receives no credentials.";
    };
    fleetSocket = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected Unix socket of the independent fleet release coordinator; no TLS credentials enter the gateway.";
    };
    engineeringSocket = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Restricted engineering daemon socket; compute and model credentials remain outside the gateway.";
    };
    executorSocket = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Narrow Unix socket of the independent credential-owning local executor.";
    };
    securityClientSocket = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected client socket of the independent owner security service. Enabling this disables legacy gateway approval access.";
    };
    consolePassphraseFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Owner-only path to the browser console passphrase verifier. This verifier must not grant cluster administration privileges.";
    };
    calendarURLFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Protected file containing the read-only Proton Calendar sharing URL.";
    };
    tokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Path to the owner-only API token outside the Nix store.";
    };
    integrations = lib.genAttrs [ "workspace" "learning" "health" ] (_: {
      url = lib.mkOption { type = lib.types.str; default = ""; };
      tokenFile = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; };
      vaultAlias = lib.mkOption { type = lib.types.str; default = ""; };
    });
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = (cfg.capabilityModel.url == "") == (cfg.capabilityModel.name == "")
          && (cfg.capabilityModel.url != "" || (cfg.capabilityModel.tokenFile == null && cfg.capabilityModel.vaultAlias == ""));
        message = "Capability planning requires a model endpoint and name; credentials cannot be configured alone.";
      }
      {
        assertion = cfg.capabilityModel.routes == [] ||
          (cfg.capabilityModel.url == "" && cfg.capabilityModel.name == "" && cfg.capabilityModel.tokenFile == null && cfg.capabilityModel.vaultAlias == "");
        message = "Purpose-specific model routes cannot be combined with legacy single-model settings.";
      }
      {
        assertion = let
          routes = cfg.capabilityModel.routes;
          purposes = map (route: route.purpose) routes;
          names = map (route: route.id) routes;
        in lib.length routes <= 3 &&
           lib.length purposes == lib.length (lib.unique purposes) &&
           lib.length names == lib.length (lib.unique names) &&
           lib.all (route: route.id != "" && route.model != "" && route.url != "") routes;
        message = "Model routing requires nonempty, unique purpose and identity records, with at most three routes.";
      }
      {
        assertion = (cfg.seerr.url == "") == (cfg.seerr.apiKeyFile == null && cfg.seerr.vaultAlias == "")
          && (cfg.seerr.apiKeyFile == null || cfg.seerr.vaultAlias == "");
        message = "Seerr requires one protected credential source and an endpoint.";
      }
      {
        assertion = (cfg.capabilityModel.tokenFile == null || cfg.capabilityModel.vaultAlias == "")
          && lib.all (route: route.tokenFile == null || route.vaultAlias == "") cfg.capabilityModel.routes
          && lib.all (name: cfg.integrations.${name}.tokenFile == null || cfg.integrations.${name}.vaultAlias == "")
            [ "workspace" "learning" "health" ];
        message = "Each external service must use only one credential authority.";
      }
      {
        assertion = let
          aliases = [ cfg.capabilityModel.vaultAlias cfg.seerr.vaultAlias ]
            ++ map (route: route.vaultAlias) cfg.capabilityModel.routes
            ++ map (name: cfg.integrations.${name}.vaultAlias) [ "workspace" "learning" "health" ];
          used = lib.any (alias: alias != "") aliases;
        in !used || (cfg.vault.endpoint != null && cfg.vault.serverName != "" &&
          cfg.vault.caFile != null && cfg.vault.certificateFile != null && cfg.vault.keyFile != null);
        message = "Vault credential aliases require pinned private TLS and client trust files.";
      }
      {
        assertion = lib.all (source: source == "" ||
          lib.hasPrefix "http://127.0.0.1:" source ||
          lib.hasPrefix "http://[::1]:" source ||
          lib.hasPrefix "http://localhost:" source)
          [ cfg.monitoring.prometheusURL cfg.monitoring.lokiURL ];
        message = "Monitoring sources must use loopback HTTP.";
      }
      {
        assertion = cfg.fleetSocket == null ||
          (cfg.securityClientSocket != null &&
           cfg.fleetSocket == "/run/homelab-fleetd/fleet.sock" &&
           cfg.integrations.workspace.tokenFile == null);
        message = "Fleet operations require the independent owner security service and exact coordinator socket without a Workspace root credential.";
      }
      {
        assertion = cfg.engineeringSocket == null || cfg.engineeringSocket == "/run/homelab-engineeringd/engineering.sock";
        message = "Engineering requires the exact local execution socket.";
      }
      {
        assertion = cfg.executorSocket == null ||
          (cfg.securityClientSocket != null && cfg.integrations.workspace.tokenFile == null);
        message = "Independent executor requires separate security authorization and no Workspace root bearer credential in the gateway.";
      }
      {
        assertion = cfg.securityLoginURL == null || (cfg.securityClientSocket != null && lib.hasPrefix "https://" cfg.securityLoginURL);
        message = "Browser authentication requires an HTTPS origin and independent security client socket.";
      }
      {
        assertion = cfg.tokenFile != null;
        message = "The platform requires a protected API token file.";
      }
      {
        assertion = lib.hasPrefix "127.0.0.1:" cfg.listenAddress
          || lib.hasPrefix "[::1]:" cfg.listenAddress
          || lib.hasPrefix "localhost:" cfg.listenAddress;
        message = "The platform must listen on loopback behind authenticated ingress.";
      }
    ];

    systemd.services.homelab-platform = {
      description = "Personal computing platform";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = {
        HOMELAB_LISTEN_ADDR = cfg.listenAddress;
        HOMELAB_ALLOWED_ORIGINS = lib.concatStringsSep "," cfg.allowedOrigins;
        HOMELAB_TIME_ZONE = cfg.timeZone;
        HOMELAB_DEVICES_FILE = pkgs.writeText "homelab-hardware.json" (builtins.toJSON cfg.devices);
        HOMELAB_NODES_FILE = pkgs.writeText "homelab-nodes.json" (builtins.toJSON (import ../hardware.nix).nodes);
        HOMELAB_APPROVAL_SOCKET = "/run/homelab-platform/approval.sock";
        HOMELAB_STATE_FILE = "/var/lib/homelab-platform/state.json";
        HOMELAB_PROFILES_FILE = "/var/lib/homelab-platform/profiles.json";
        HOMELAB_RECIPES_FILE = "/var/lib/homelab-platform/recipes.json";
        HOMELAB_API_TOKEN_FILE = "/run/credentials/homelab-platform.service/access_token";
        HOMELAB_PROMETHEUS_URL = cfg.monitoring.prometheusURL;
        HOMELAB_LOKI_URL = cfg.monitoring.lokiURL;
      } // lib.optionalAttrs (cfg.capabilityModel.routes != []) {
        HOMELAB_CAPABILITY_MODEL_ROUTES_FILE = pkgs.writeText "eminence-model-routes.json" (builtins.toJSON {
          routes = lib.imap0 (index: route: {
            name = route.id;
            purpose = route.purpose;
            endpoint = route.url;
            model = route.model;
            local_only = route.localOnly;
            token_file = if route.tokenFile == null then "" else
              "/run/credentials/homelab-platform.service/model-route-token-${toString index}";
            vault_alias = route.vaultAlias;
          }) cfg.capabilityModel.routes;
        });
      } // lib.optionalAttrs (cfg.capabilityModel.url != "") {
        HOMELAB_CAPABILITY_MODEL_URL = cfg.capabilityModel.url;
        HOMELAB_CAPABILITY_MODEL_NAME = cfg.capabilityModel.name;
      } // lib.optionalAttrs (cfg.capabilityModel.tokenFile != null) {
        HOMELAB_CAPABILITY_MODEL_TOKEN_FILE = "/run/credentials/homelab-platform.service/capability-model-token";
      } // lib.optionalAttrs (cfg.capabilityModel.vaultAlias != "") {
        HOMELAB_CAPABILITY_MODEL_VAULT_ALIAS = cfg.capabilityModel.vaultAlias;
      } // lib.optionalAttrs (cfg.vault.endpoint != null) {
        HOMELAB_VAULT_URL = cfg.vault.endpoint;
        HOMELAB_VAULT_SERVER_NAME = cfg.vault.serverName;
        HOMELAB_VAULT_CA_FILE = "/run/credentials/homelab-platform.service/vault-ca";
        HOMELAB_VAULT_CLIENT_CERT_FILE = "/run/credentials/homelab-platform.service/vault-cert";
        HOMELAB_VAULT_CLIENT_KEY_FILE = "/run/credentials/homelab-platform.service/vault-key";
      } // lib.optionalAttrs (cfg.securityClientSocket != null) {
        HOMELAB_SECURITY_SOCKET = cfg.securityClientSocket;
      } // lib.optionalAttrs (cfg.securityLoginURL != null) {
        HOMELAB_SECURITY_LOGIN_URL = cfg.securityLoginURL;
      } // lib.optionalAttrs (cfg.executorSocket != null) {
        HOMELAB_EXECUTOR_SOCKET = cfg.executorSocket;
      } // lib.optionalAttrs (cfg.engineeringSocket != null) {
        HOMELAB_ENGINEERING_SOCKET = cfg.engineeringSocket;
      } // lib.optionalAttrs (cfg.fleetSocket != null) {
        HOMELAB_FLEET_SOCKET = cfg.fleetSocket;
      } // lib.optionalAttrs (cfg.books.contactFile != null) {
        HOMELAB_BOOKS_CONTACT_FILE = "/run/credentials/homelab-platform.service/books_contact";
      } // lib.optionalAttrs (cfg.consolePassphraseFile != null) {
        HOMELAB_CONSOLE_PASSPHRASE_FILE = "/run/credentials/homelab-platform.service/console_passphrase";
      } // lib.optionalAttrs (cfg.seerr.url != "") {
        HOMELAB_SEERR_URL = cfg.seerr.url;
      } // lib.optionalAttrs (cfg.seerr.apiKeyFile != null) {
        HOMELAB_SEERR_API_KEY_FILE = "/run/credentials/homelab-platform.service/seerr_api_key";
      } // lib.optionalAttrs (cfg.seerr.vaultAlias != "") {
        HOMELAB_SEERR_VAULT_ALIAS = cfg.seerr.vaultAlias;
      } // lib.optionalAttrs (cfg.calendarURLFile != null) {
        HOMELAB_CALENDAR_URL_FILE = "/run/credentials/homelab-platform.service/calendar_url";
      } // (lib.foldl' (acc: name:
        let
          integration = cfg.integrations.${name};
          upper = lib.toUpper name;
        in acc
          // lib.optionalAttrs (integration.url != "") {
            "HOMELAB_${upper}_MCP_URL" = integration.url;
          }
          // lib.optionalAttrs (integration.tokenFile != null) {
            "HOMELAB_${upper}_MCP_TOKEN_FILE" =
              "/run/credentials/homelab-platform.service/${name}_token";
          }
          // lib.optionalAttrs (integration.vaultAlias != "") {
            "HOMELAB_${upper}_MCP_VAULT_ALIAS" = integration.vaultAlias;
          }
      ) {} [ "workspace" "learning" "health" ]);
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        SupplementaryGroups = lib.optional (cfg.engineeringSocket != null) "homelab-engineering-client"
          ++ lib.optional (cfg.securityClientSocket != null) "homelab-security-client"
          ++ lib.optional (cfg.executorSocket != null) "homelab-executor-client"
          ++ lib.optional (cfg.fleetSocket != null) "homelab-fleet-client";
        StateDirectory = "homelab-platform";
        RuntimeDirectory = "homelab-platform";
        RuntimeDirectoryMode = "0700";
        StateDirectoryMode = "0700";
        LoadCredential =
          [ "access_token:${cfg.tokenFile}" ]
          ++ lib.optional (cfg.calendarURLFile != null) "calendar_url:${cfg.calendarURLFile}"
          ++ lib.optional (cfg.consolePassphraseFile != null) "console_passphrase:${cfg.consolePassphraseFile}"
          ++ lib.optional (cfg.books.contactFile != null) "books_contact:${cfg.books.contactFile}"
          ++ lib.optional (cfg.seerr.apiKeyFile != null) "seerr_api_key:${cfg.seerr.apiKeyFile}"
          ++ lib.optionals (cfg.vault.endpoint != null) [
            "vault-ca:${cfg.vault.caFile}"
            "vault-cert:${cfg.vault.certificateFile}"
            "vault-key:${cfg.vault.keyFile}"
          ]
          ++ lib.optional (cfg.capabilityModel.tokenFile != null) "capability-model-token:${cfg.capabilityModel.tokenFile}"
          ++ lib.concatLists (lib.imap0 (index: route:
            lib.optional (route.tokenFile != null)
              "model-route-token-${toString index}:${route.tokenFile}"
          ) cfg.capabilityModel.routes)
          ++ lib.concatMap (name:
            lib.optional (cfg.integrations.${name}.tokenFile != null)
              "${name}_token:${cfg.integrations.${name}.tokenFile}"
          ) [ "workspace" "learning" "health" ];
        ExecStart = "${cfg.package}/bin/homelab-platform";
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
      };
    };
  };
}
