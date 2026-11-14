{ config, lib, pkgs, ... }:
let
  cfg = config.homelab.personalHealth;
  executable = if cfg.package != null then "${cfg.package}/bin/personal-health-mcp" else if cfg.binaryPath != null then cfg.binaryPath else "/bin/false";
  checksum = pkgs.writeText "personal-health-mcp-release.sha256"
    "${cfg.binarySHA256}  ${executable}\n";
in {
  options.homelab.personalHealth = {
    enable = lib.mkEnableOption "Personal Health MCP";
    package = lib.mkOption {
      type = lib.types.nullOr lib.types.package;
      default = null;
      description = "Immutable Nix package providing bin/personal-health-mcp.";
    };
    binaryPath = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Exact, SHA-256-pinned executable built from the unified source release or the approved standalone health release.";
    };
    binarySHA256 = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Exact SHA-256 of the release executable.";
    };
    environmentFile = lib.mkOption {
      type = lib.types.str;
      default = "/srv/homelab/secrets/personal-health-mcp.env";
      description = "Root-only bearer and optional RecipeSage credentials.";
    };
    access = {
      issuer = lib.mkOption { type = lib.types.str; };
      audience = lib.mkOption { type = lib.types.str; };
      certsURL = lib.mkOption { type = lib.types.str; };
      ownerCredentialFile = lib.mkOption { type = lib.types.str; };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = (cfg.package != null) != (cfg.binaryPath != null) &&
          (cfg.binaryPath == null || lib.hasPrefix "/srv/releases/personal-health-mcp/" cfg.binaryPath ||
           (lib.hasPrefix "/srv/releases/homelab/" cfg.binaryPath &&
            lib.hasSuffix "/bin/homelab-health" cfg.binaryPath));
        message = "Personal Health MCP requires an immutable release executable.";
      }
      {
        assertion = cfg.binaryPath == null || builtins.match "[0-9a-f]{64}" cfg.binarySHA256 != null;
        message = "Personal Health MCP requires a SHA-256 release checksum.";
      }
    ];

    systemd.services.personal-health-mcp = {
      description = "Personal Health MCP";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" "saturn-recipesage.service" ];
      wants = [ "network-online.target" "saturn-recipesage.service" ];
      environment = {
        MCP_TRANSPORT = "http";
        MCP_LISTEN = "127.0.0.1:8201";
        MCP_ALLOWED_ORIGINS = "https://chatgpt.com,https://chat.openai.com";
        PERSONAL_HEALTH_DB_PATH = "/var/lib/personal-health/health.db";
        RECIPE_SAGE_ALLOW_HTTP = "true";
        PERSONAL_HEALTH_ACCESS_ISSUER = cfg.access.issuer;
        PERSONAL_HEALTH_ACCESS_AUDIENCE = cfg.access.audience;
        PERSONAL_HEALTH_ACCESS_CERTS_URL = cfg.access.certsURL;
        PERSONAL_HEALTH_ACCESS_OWNER_FILE =
          "/run/credentials/personal-health-mcp.service/access_owner";
      };
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        StateDirectory = "personal-health";
        StateDirectoryMode = "0700";
        EnvironmentFile = "-${cfg.environmentFile}";
        LoadCredential = [ "access_owner:${cfg.access.ownerCredentialFile}" ];
        ExecStartPre = lib.optional (cfg.binaryPath != null) "${pkgs.coreutils}/bin/sha256sum --check ${checksum}";
        ExecStart = executable;
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
