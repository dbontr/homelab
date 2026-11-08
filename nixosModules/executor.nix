{ config, lib, pkgs, ... }:
let
  cfg = config.services.homelabExecutor;
  package = import ../packages/executor.nix { inherit pkgs; };
in
{
  options.services.homelabExecutor = {
    enable = lib.mkEnableOption "restricted independent local executor";
    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      description = "Executor and guarded NixOS release controller.";
    };
    sourceDirectory = lib.mkOption {
      type = lib.types.str;
      default = "/srv/homelab-new";
      description = "Trusted local Git checkout used only by the offline release stager.";
    };
    stateDirectory = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/homelab-releases";
      description = "Private release journal and staged-generation metadata directory.";
    };
    allowReleaseActivation = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Explicitly allow an owner-confirmed local NixOS generation switch through the executor.";
    };
    authorization.remote = {
      enable = lib.mkEnableOption "central owner-session authorization over mutual TLS";
      endpoint = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Fixed HTTPS security service origin, without a user-supplied route.";
      };
      serverName = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Exact expected DNS identity of the central security service.";
      };
      certificateFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected executor client certificate.";
      };
      keyFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected executor client private key.";
      };
      serverCAFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected trust roots for the central security service.";
      };
    };
    remote = {
      enable = lib.mkEnableOption "mutually authenticated private release-agent endpoint";
      listenAddress = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Explicit private IP address and TCP port. Never use a wildcard or public IP.";
      };
      certificateFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected host TLS certificate chain.";
      };
      keyFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected host TLS private key.";
      };
      clientCAFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected CA roots for verifying trusted coordinator certificates.";
      };
      coordinatorDNS = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Exact DNS subject alternative name required in the coordinator client certificate.";
      };
    };
    workspace = {
      enable = lib.mkEnableOption "restricted Workspace controller administrative operations";
      endpoint = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Fixed HTTPS Workspace MCP endpoint, or loopback HTTP for a colocated controller.";
      };
      rootTokenFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Protected controller root token loaded only into the privileged executor.";
      };
      projects = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
      workers = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
      images = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
      software = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
      packages = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; };
    };
    kubernetes = {
      enable = lib.mkEnableOption "explicit Kubernetes deployment operations";
      kubeconfigFile = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "Root-owned kubeconfig loaded only into the executor through systemd credentials.";
      };
      namespaces = lib.mkOption {
        type = lib.types.attrsOf (lib.types.listOf lib.types.str);
        default = {};
        description = "Exact namespace and deployment names authorized for typed Kubernetes changes.";
      };
      maxReplicas = lib.mkOption {
        type = lib.types.int;
        default = 16;
        description = "Maximum allowed replica count for a single deployment scale action.";
      };
    };
    allowedServices = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Exact non-critical local systemd units permitted for owner-approved start, stop, reload and restart operations.";
    };
    healthServices = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Non-empty service readiness list required for activation and rollback.";
    };
  };
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.authorization.remote.enable ||
          (config.services.homelabSecurity.enable &&
           config.services.homelabSecurity.access.gatewayAudience != "");
        message = "Independent executor requires signed owner verification, local or through central security.";
      }
      {
        assertion = !cfg.authorization.remote.enable ||
          (cfg.authorization.remote.endpoint != null &&
           cfg.authorization.remote.serverName != "" &&
           cfg.authorization.remote.certificateFile != null &&
           cfg.authorization.remote.keyFile != null &&
           cfg.authorization.remote.serverCAFile != null);
        message = "Central authorization requires an HTTPS endpoint, pinned server name, certificate and trust roots.";
      }
      {
        assertion = !cfg.remote.enable ||
          (cfg.remote.listenAddress != null &&
           cfg.remote.certificateFile != null && cfg.remote.keyFile != null &&
           cfg.remote.clientCAFile != null && cfg.remote.coordinatorDNS != "");
        message = "Remote executor requires an explicit private listener, TLS certificate, key, client CA and coordinator identity.";
      }
      {
        assertion = cfg.healthServices != [];
        message = "Independent NixOS releases require at least one health service.";
      }
      {
        assertion = !cfg.workspace.enable ||
          (cfg.workspace.endpoint != "" && cfg.workspace.rootTokenFile != null &&
           (cfg.workspace.projects != [] || cfg.workspace.workers != [] ||
            cfg.workspace.images != [] || cfg.workspace.software != [] ||
            cfg.workspace.packages != []));
        message = "Workspace executor requires a fixed endpoint, protected controller credential and explicit resource allowlist.";
      }
      {
        assertion = !cfg.kubernetes.enable ||
          (cfg.kubernetes.kubeconfigFile != null && cfg.kubernetes.namespaces != {} &&
           cfg.kubernetes.maxReplicas >= 1 && cfg.kubernetes.maxReplicas <= 1000);
        message = "Kubernetes executor requires a kubeconfig, exact resource allowlist and bounded replicas.";
      }
      {
        assertion = lib.hasPrefix "/" cfg.sourceDirectory &&
          lib.hasPrefix "/" cfg.stateDirectory && cfg.stateDirectory != "/";
        message = "Executor source and release state require absolute paths.";
      }
      {
        assertion = cfg.authorization.remote.enable || config.services.homelabPlatform.securityClientSocket != null;
        message = "Platform must use independent authorization before enabling the executor.";
      }
      {
        assertion = cfg.authorization.remote.enable || config.services.homelabPlatform.executorSocket == "/run/homelab-executord/execute.sock";
        message = "Platform executor socket must match the protected local executor endpoint.";
      }
    ];
    users.groups.homelab-executor-client = {};
    environment.systemPackages = [ cfg.package (pkgs.writeShellScriptBin "homelab-releasectl" ''
      export HOMELAB_RELEASE_HOST=${lib.escapeShellArg config.networking.hostName}
      export HOMELAB_RELEASE_SOURCE=${lib.escapeShellArg cfg.sourceDirectory}
      export HOMELAB_RELEASE_STATE=${lib.escapeShellArg cfg.stateDirectory}
      export HOMELAB_RELEASE_NIX=${lib.escapeShellArg ("${pkgs.nix}/bin/nix")}
      export HOMELAB_RELEASE_NIX_ENV=${lib.escapeShellArg ("${pkgs.nix}/bin/nix-env")}
      export HOMELAB_RELEASE_GIT=${lib.escapeShellArg ("${pkgs.git}/bin/git")}
      export HOMELAB_RELEASE_SYSTEMCTL=${lib.escapeShellArg ("${pkgs.systemd}/bin/systemctl")}
      export HOMELAB_RELEASE_HEALTH_FILE=${lib.escapeShellArg (pkgs.writeText "homelab-release-health.json" (builtins.toJSON cfg.healthServices))}
      exec ${cfg.package}/bin/homelab-release "$@"
    '') ];
    systemd.services.homelab-executord = {
      description = "Restricted local infrastructure executor";
      wantedBy = [ "multi-user.target" ];
      requires = lib.optionals (!cfg.authorization.remote.enable) [ "homelab-securityd.service" ];
      after = lib.optionals (!cfg.authorization.remote.enable) [ "homelab-securityd.service" ];
      environment = {
        HOMELAB_EXECUTOR_HOST = config.networking.hostName;
        HOMELAB_EXECUTOR_RELEASE_ENABLE = if cfg.allowReleaseActivation then "true" else "false";
        HOMELAB_EXECUTOR_SECURITY_SOCKET = "/run/homelab-securityd/client.sock";
        HOMELAB_EXECUTOR_SOURCE = cfg.sourceDirectory;
        HOMELAB_EXECUTOR_RELEASE_STATE = cfg.stateDirectory;
        HOMELAB_EXECUTOR_NIX = "${pkgs.nix}/bin/nix";
        HOMELAB_EXECUTOR_NIX_ENV = "${pkgs.nix}/bin/nix-env";
        HOMELAB_EXECUTOR_GIT = "${pkgs.git}/bin/git";
        HOMELAB_EXECUTOR_SYSTEMCTL = "${pkgs.systemd}/bin/systemctl";
        HOMELAB_EXECUTOR_SERVICES_FILE = pkgs.writeText "homelab-executor-services.json" (builtins.toJSON cfg.allowedServices);
        HOMELAB_EXECUTOR_HEALTH_FILE = pkgs.writeText "homelab-executor-health.json" (builtins.toJSON cfg.healthServices);
      } // lib.optionalAttrs cfg.authorization.remote.enable {
        HOMELAB_EXECUTOR_AUTH_ENDPOINT = cfg.authorization.remote.endpoint;
        HOMELAB_EXECUTOR_AUTH_SERVER_NAME = cfg.authorization.remote.serverName;
        HOMELAB_EXECUTOR_AUTH_CERT = "/run/credentials/homelab-executord.service/auth-cert";
        HOMELAB_EXECUTOR_AUTH_KEY = "/run/credentials/homelab-executord.service/auth-key";
        HOMELAB_EXECUTOR_AUTH_CA = "/run/credentials/homelab-executord.service/auth-ca";
      } // lib.optionalAttrs cfg.remote.enable {
        HOMELAB_EXECUTOR_REMOTE_LISTEN = cfg.remote.listenAddress;
        HOMELAB_EXECUTOR_REMOTE_CERT = "/run/credentials/homelab-executord.service/remote-cert";
        HOMELAB_EXECUTOR_REMOTE_KEY = "/run/credentials/homelab-executord.service/remote-key";
        HOMELAB_EXECUTOR_REMOTE_CA = "/run/credentials/homelab-executord.service/remote-ca";
        HOMELAB_EXECUTOR_REMOTE_COORDINATOR = cfg.remote.coordinatorDNS;
      } // lib.optionalAttrs cfg.workspace.enable {
        HOMELAB_EXECUTOR_WORKSPACE_TOKEN = "/run/credentials/homelab-executord.service/workspace-token";
        HOMELAB_EXECUTOR_WORKSPACE_POLICY_FILE = pkgs.writeText "homelab-workspace-policy.json" (builtins.toJSON {
          Endpoint = cfg.workspace.endpoint;
          Projects = cfg.workspace.projects;
          Workers = cfg.workspace.workers;
          Images = cfg.workspace.images;
          Software = cfg.workspace.software;
          Packages = cfg.workspace.packages;
        });
      } // lib.optionalAttrs cfg.kubernetes.enable {
        HOMELAB_EXECUTOR_KUBECTL = "${pkgs.kubectl}/bin/kubectl";
        HOMELAB_EXECUTOR_KUBECONFIG = "/run/credentials/homelab-executord.service/kubeconfig";
        HOMELAB_EXECUTOR_KUBERNETES_POLICY_FILE = pkgs.writeText "homelab-kubernetes-policy.json" (builtins.toJSON {
          Namespaces = cfg.kubernetes.namespaces;
          MaxReplicas = cfg.kubernetes.maxReplicas;
        });
      };
      serviceConfig = {
        Type = "simple";
        User = "root";
        Group = "root";
        SupplementaryGroups = lib.optionals (!cfg.authorization.remote.enable) [ "homelab-security-client" ];
        StateDirectory = "homelab-releases";
        StateDirectoryMode = "0700";
        RuntimeDirectory = "homelab-executord";
        RuntimeDirectoryMode = "0750";
        ExecStart = "${cfg.package}/bin/homelab-executord";
        LoadCredential = lib.optionals cfg.authorization.remote.enable [
          "auth-cert:${cfg.authorization.remote.certificateFile}"
          "auth-key:${cfg.authorization.remote.keyFile}"
          "auth-ca:${cfg.authorization.remote.serverCAFile}"
        ] ++ lib.optionals cfg.kubernetes.enable [
          "kubeconfig:${cfg.kubernetes.kubeconfigFile}"
        ] ++ lib.optionals cfg.workspace.enable [
          "workspace-token:${cfg.workspace.rootTokenFile}"
        ] ++ lib.optionals cfg.remote.enable [
          "remote-cert:${cfg.remote.certificateFile}"
          "remote-key:${cfg.remote.keyFile}"
          "remote-ca:${cfg.remote.clientCAFile}"
        ];
        Restart = "on-failure";
        RestartSec = "5s";
        UMask = "0077";
        NoNewPrivileges = !cfg.allowReleaseActivation;
        PrivateNetwork = !(cfg.kubernetes.enable || cfg.allowReleaseActivation || cfg.remote.enable || cfg.authorization.remote.enable || cfg.workspace.enable);
        PrivateTmp = true;
        ProtectSystem = if cfg.allowReleaseActivation then false else "strict";
        ReadWritePaths = [ "/nix/var/nix/profiles" cfg.stateDirectory ];
        ProtectHome = true;
        PrivateDevices = !cfg.allowReleaseActivation;
        ProtectKernelTunables = !cfg.allowReleaseActivation;
        ProtectKernelModules = !cfg.allowReleaseActivation;
        ProtectControlGroups = !cfg.allowReleaseActivation;
        CapabilityBoundingSet = lib.mkIf (!cfg.allowReleaseActivation) "";
        RestrictAddressFamilies = [ "AF_UNIX" ] ++ lib.optionals (cfg.kubernetes.enable || cfg.allowReleaseActivation || cfg.remote.enable || cfg.authorization.remote.enable || cfg.workspace.enable) [ "AF_INET" "AF_INET6" ];
        RestrictNamespaces = !cfg.allowReleaseActivation;
        RestrictSUIDSGID = true;
        LockPersonality = true;
      };
    };
  };
}
