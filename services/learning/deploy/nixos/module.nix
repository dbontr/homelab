{ config, lib, pkgs, ... }:
let
  cfg = config.services.adaptiveTeachingMcp;
  executable = if cfg.package != null then "${cfg.package}/bin/adaptive-teaching" else if cfg.binaryPath != null then cfg.binaryPath else "/bin/false";
  checksum = pkgs.writeText "adaptive-teaching-release.sha256" "${cfg.binarySHA256}  ${if cfg.binaryPath == null then "/bin/false" else cfg.binaryPath}\n";
  credentialFile = if cfg.bearerCredentialFile == null then "/dev/null" else cfg.bearerCredentialFile;
in {
  options.services.adaptiveTeachingMcp = {
    enable = lib.mkEnableOption "the independently packaged Adaptive Teaching MCP";
    package = lib.mkOption {
      type = lib.types.nullOr lib.types.package;
      default = null;
      description = "Verified upstream release containing bin/adaptive-teaching.";
    };
    binaryPath = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Immutable release executable on the host, installed from an authenticated upstream artifact.";
    };
    binarySHA256 = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Expected SHA-256 of the separately installed release executable.";
    };
    bearerCredentialFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Runtime path to an owner-only MCP bearer token, kept outside the Nix store.";
    };
    appBundle = {
      directory = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Verified immutable Rust/WASM Learning MCP App release directory.";
      };
      origin = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Exact public HTTPS origin serving verified Learning App assets.";
      };
    };
    model = {
      allowDisclosure = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = "Permit consented learner responses and source passages to leave the Learning service for the configured model.";
      };
      endpoint = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Operator-selected HTTPS or loopback chat-completions endpoint for optional model proposals.";
      };
      name = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Exact model identifier used only for provisional Learning assessment and extraction.";
      };
      tokenCredentialFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Optional owner-only API credential, never stored in the Nix store.";
      };
    };
    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8200";
      description = "Loopback address for the authenticated local MCP service.";
    };
    allowedHosts = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:*,localhost:*";
      description = "Comma-separated HTTP Host allowlist for the trusted local ingress.";
    };
    allowedOrigins = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Comma-separated allowed browser Origin values.";
    };
    access = {
      issuer = lib.mkOption {
        type = lib.types.str;
        default = "";
      };
      audience = lib.mkOption {
        type = lib.types.str;
        default = "";
      };
      certsURL = lib.mkOption {
        type = lib.types.str;
        default = "";
      };
      ownerCredentialFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Root-owned file containing the authorized Access identity.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = (cfg.package != null) != (cfg.binaryPath != null);
        message = "Adaptive Teaching requires exactly one release source: a Nix package or an immutable host executable.";
      }
      {
        assertion = cfg.binaryPath == null || builtins.match "[0-9a-f]{64}" cfg.binarySHA256 != null;
        message = "Adaptive Teaching host releases require an exact SHA-256.";
      }
      {
        assertion = cfg.bearerCredentialFile != null;
        message = "Adaptive Teaching requires an external bearer token file.";
      }
      {
        assertion = (cfg.model.endpoint == null) == (cfg.model.name == null) &&
          (cfg.model.tokenCredentialFile == null || cfg.model.endpoint != null) &&
          (!cfg.model.allowDisclosure || cfg.model.endpoint != null);
        message = "Learning model requires endpoint and name together; credentials and disclosure require a configured provider.";
      }
      {
        assertion = (cfg.appBundle.directory == null) == (cfg.appBundle.origin == null);
        message = "Learning App requires verified assets and their public origin together.";
      }
      {
        assertion = cfg.appBundle.origin == null ||
          builtins.match "https://[A-Za-z0-9.-]+(:[0-9]+)?" cfg.appBundle.origin != null;
        message = "Learning App assets require an exact HTTPS origin.";
      }
      {
        assertion = cfg.model.endpoint == null ||
          builtins.match "https://[^/?#]+/v1/chat/completions" cfg.model.endpoint != null ||
          builtins.match "http://(127[.]0[.]0[.]1|localhost|\\[::1\\])(:[0-9]+)?/v1/chat/completions" cfg.model.endpoint != null;
        message = "Learning model endpoint must use HTTPS or an explicit loopback HTTP host.";
      }
      {
        assertion = (cfg.access.issuer == "" && cfg.access.audience == "" &&
          cfg.access.certsURL == "" && cfg.access.ownerCredentialFile == null) ||
          (cfg.access.issuer != "" && cfg.access.audience != "" &&
          cfg.access.certsURL != "" && cfg.access.ownerCredentialFile != null);
        message = "Adaptive Teaching Access identity requires all verification fields and an owner credential.";
      }
      {
        assertion = lib.hasPrefix "127.0.0.1:" cfg.listenAddress ||
          lib.hasPrefix "[::1]:" cfg.listenAddress ||
          lib.hasPrefix "localhost:" cfg.listenAddress;
        message = "Adaptive Teaching must listen on loopback behind the trusted ingress.";
      }
    ];

    systemd.services.adaptive-teaching = {
      description = "Adaptive Teaching MCP";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        StateDirectory = "adaptive-teaching";
        StateDirectoryMode = "0700";
        RuntimeDirectory = "adaptive-teaching";
        RuntimeDirectoryMode = "0700";
        LoadCredential = [ "mcp_token:${credentialFile}" ] ++
          lib.optional (cfg.access.ownerCredentialFile != null)
            "access_owner:${cfg.access.ownerCredentialFile}" ++
          lib.optional (cfg.model.tokenCredentialFile != null)
            "model_token:${cfg.model.tokenCredentialFile}";
        ExecStart = executable;
        ExecStartPre = (lib.optional (cfg.binaryPath != null)
          "${pkgs.coreutils}/bin/sha256sum --check ${checksum}") ++ [
            "${pkgs.coreutils}/bin/install -m 0600 %d/mcp_token /run/adaptive-teaching/mcp_token"
          ] ++ lib.optional (cfg.access.ownerCredentialFile != null)
            "${pkgs.coreutils}/bin/install -m 0600 %d/access_owner /run/adaptive-teaching/access_owner" ++
          lib.optional (cfg.model.tokenCredentialFile != null)
            "${pkgs.coreutils}/bin/install -m 0600 %d/model_token /run/adaptive-teaching/model_token";
        Environment = [
          "ADAPTIVE_TEACHING_DB_PATH=/var/lib/adaptive-teaching/learning.db"
          "ADAPTIVE_TEACHING_LISTEN_ADDR=${cfg.listenAddress}"
          "ADAPTIVE_TEACHING_ALLOWED_HOSTS=${cfg.allowedHosts}"
          "ADAPTIVE_TEACHING_ALLOWED_ORIGINS=${cfg.allowedOrigins}"
          "ADAPTIVE_TEACHING_BEARER_TOKEN_FILE=/run/adaptive-teaching/mcp_token"
        ] ++ lib.optionals (cfg.appBundle.directory != null) [
          "ADAPTIVE_TEACHING_APP_BUNDLE_DIRECTORY=${cfg.appBundle.directory}"
          "ADAPTIVE_TEACHING_APP_BUNDLE_ORIGIN=${cfg.appBundle.origin}"
        ] ++ lib.optionals (cfg.model.endpoint != null) [
          "ADAPTIVE_TEACHING_MODEL_ENDPOINT=${cfg.model.endpoint}"
          "ADAPTIVE_TEACHING_MODEL_NAME=${cfg.model.name}"
        ] ++ lib.optional cfg.model.allowDisclosure
          "ADAPTIVE_TEACHING_MODEL_DISCLOSURE_ENABLED=1" ++
          lib.optional (cfg.model.tokenCredentialFile != null)
          "ADAPTIVE_TEACHING_MODEL_TOKEN_FILE=/run/adaptive-teaching/model_token" ++
          lib.optionals (cfg.access.ownerCredentialFile != null) [
          "ADAPTIVE_TEACHING_ACCESS_ISSUER=${cfg.access.issuer}"
          "ADAPTIVE_TEACHING_ACCESS_AUDIENCE=${cfg.access.audience}"
          "ADAPTIVE_TEACHING_ACCESS_CERTS_URL=${cfg.access.certsURL}"
          "ADAPTIVE_TEACHING_ACCESS_OWNER_FILE=/run/adaptive-teaching/access_owner"
        ];
        Restart = "on-failure";
        RestartSec = "5s";
        TimeoutStopSec = "20s";
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
