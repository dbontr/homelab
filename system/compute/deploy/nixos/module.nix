{ config, lib, pkgs, ... }:
let
  cfg = config.services.workspaceMcp;
  chartPath = toString cfg.chart;
  controllerValues = pkgs.writeText "workspace-controller-values.json" (builtins.toJSON {
    systemNamespace = "pwm-system";
    controller = {
      enabled = true;
      image = cfg.controllerImage;
      imagePullPolicy = "IfNotPresent";
      imagePullSecrets = map (name: { inherit name; }) cfg.imagePullSecretNames;
      nodeSelector = lib.optionalAttrs (cfg.controllerNode != "") { "kubernetes.io/hostname" = cfg.controllerNode; };
      databaseSize = cfg.databaseSize;
      storageClass = cfg.storageClass;
      backupInterval = cfg.backupInterval;
      embedding = {
        url = cfg.embedding.url;
        model = cfg.embedding.model;
        tokenSecretName = cfg.embedding.tokenSecretName;
      };
      offsite = {
        repository = cfg.offsite.repository;
        passwordSecretName = cfg.offsite.passwordSecretName;
        backendSecretName = cfg.offsite.backendSecretName;
        sshSecretName = cfg.offsite.sshSecretName;
      };
      allowedHosts = cfg.allowedHosts;
      allowedOrigins = cfg.allowedOrigins;
      environmentRegistry = cfg.environmentRegistry;
      publicURL = cfg.publicURL;
      githubPATSecretName = cfg.githubPATSecretName;
      gitlabPATSecretName = cfg.gitlabPATSecretName;
      gitlabAPIURL = cfg.gitlabAPIURL;
      vault = {
        enabled = cfg.vault.enable;
        endpoint = cfg.vault.endpoint;
        serverName = cfg.vault.serverName;
        caSecretName = cfg.vault.caSecretName;
        clientIdentitySecretName = cfg.vault.clientIdentitySecretName;
        githubAlias = cfg.vault.githubAlias;
        gitlabAlias = cfg.vault.gitlabAlias;
        embeddingsAlias = cfg.vault.embeddingsAlias;
      };
      access = {
        issuer = cfg.access.issuer;
        audience = cfg.access.audience;
        jwksURL = cfg.access.jwksURL;
        ownerEmailSecretName = cfg.access.ownerEmailSecretName;
      };
    };
    worker.enabled = false;
  });
  workerValues = name: worker: pkgs.writeText "pwm-${name}-values.json" (builtins.toJSON {
    systemNamespace = "pwm-system";
    controller.enabled = false;
    worker = {
      enabled = true;
      image = cfg.workerImage;
      imagePullPolicy = "IfNotPresent";
      nodeSelector = worker.nodeSelector;
      workloadNode = worker.workloadNode;
      imagePullSecrets = map (secretName: { name = secretName; }) cfg.imagePullSecretNames;
      name = worker.name;
      projectID = worker.projectID;
      instanceID = worker.instanceID;
      workloadNamespace = worker.workloadNamespace;
      tokenSecretName = worker.tokenSecretName;
      controllerCASecretName = "pwm-controller-ca";
      cpuCapacity = worker.cpuCapacity;
      memoryCapacityMB = worker.memoryCapacityMB;
      maxJobs = worker.maxJobs;
      workspaceStorage = worker.workspaceStorage;
      workspaceStorageClass = worker.workspaceStorageClass;
      deviceManifestConfigMap = worker.deviceManifestConfigMap;
      gpuRuntimeClassName = worker.gpuRuntimeClassName;
      allowedImages = lib.concatStringsSep "," worker.allowedImages;
      workspaceImagePullSecret = worker.workspaceImagePullSecret;
      builder = worker.builder;
    };
    quota = worker.quota;
  });
  imageIsPinned = image:
    image != null && builtins.match ".+@sha256:[a-f0-9]{64}" image != null;
  workerType = lib.types.submodule ({ name, ... }: {
    options = {
      name = lib.mkOption {
        type = lib.types.str;
        default = "${name}-agent";
        description = "Identity of the separately paired Kubernetes worker.";
      };
      projectID = lib.mkOption {
        type = lib.types.str;
        default = name;
        description = "Existing controller project identifier.";
      };
      instanceID = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Unique Kubernetes resource identity for another worker in the same project; empty preserves original project-derived names.";
      };
      workloadNamespace = lib.mkOption {
        type = lib.types.str;
        default = "pwm-${name}";
        description = "Worker-scoped namespace for Kubernetes jobs and PVCs.";
      };
      nodeSelector = lib.mkOption {
        type = lib.types.attrsOf lib.types.str;
        default = {};
        description = "Explicit Kubernetes node placement for this worker service.";
      };
      workloadNode = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Optional hostname required for every workspace Pod and batch Job managed by this worker.";
      };
      tokenSecretName = lib.mkOption {
        type = lib.types.str;
        default = "pwm-worker-${name}-credentials";
        description = "Preprovisioned Secret with the paired worker_token key.";
      };
      cpuCapacity = lib.mkOption {
        type = lib.types.int;
        default = 2;
      };
      memoryCapacityMB = lib.mkOption {
        type = lib.types.int;
        default = 2048;
      };
      maxJobs = lib.mkOption {
        type = lib.types.int;
        default = 1;
      };
      workspaceStorage = lib.mkOption {
        type = lib.types.str;
        default = "10Gi";
      };
      workspaceStorageClass = lib.mkOption {
        type = lib.types.str;
        default = "";
      };
      deviceManifestConfigMap = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Approved Kubernetes device-plugin inventory ConfigMap name.";
      };
      gpuRuntimeClassName = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "RuntimeClass used only for approved GPU requests.";
      };
      quota = lib.mkOption {
        type = lib.types.attrsOf lib.types.str;
        default = { cpu = "16"; memory = "32Gi"; storage = "500Gi"; maxPods = "64"; maxPVCs = "64"; maxJobs = "64"; };
        description = "Namespace ResourceQuota values for the worker.";
      };
      allowedImages = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ "debian:bookworm-slim" ];
        description = "Permitted workload images. The runtime enforces this allowlist.";
      };
      workspaceImagePullSecret = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Registry pull Secret in the worker's project namespace.";
      };
      builder = lib.mkOption {
        type = lib.types.submodule {
          options = {
            image = lib.mkOption { type = lib.types.str; default = ""; description = "Digest-pinned rootless OCI builder image."; };
            registry = lib.mkOption { type = lib.types.str; default = ""; description = "Approved HTTPS registry and repository prefix for built images."; };
            authSecretName = lib.mkOption { type = lib.types.str; default = ""; description = "Existing dockerconfigjson Secret in the project namespace."; };
          };
        };
        default = {};
        description = "Optional registry-backed image builder for this worker.";
      };
    };
  });
in {
  options.services.workspaceMcp = {
    enable = lib.mkEnableOption "source-owned Compute Kubernetes deployments";
    chart = lib.mkOption {
      type = lib.types.path;
      default = ../helm/project-workspace;
      description = "Local source-owned Compute controller and worker Helm chart.";
    };
    controllerImage = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Immutable controller OCI image reference including SHA-256 digest.";
    };
    workerImage = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Immutable worker OCI image reference including SHA-256 digest.";
    };
    imagePullSecretNames = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Preprovisioned registry pull Secrets in pwm-system.";
    };
    controllerNode = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Kubernetes node hosting the controller and its persistent volume.";
    };
    databaseSize = lib.mkOption {
      type = lib.types.str;
      default = "50Gi";
    };
    storageClass = lib.mkOption {
      type = lib.types.str;
      default = "";
    };
    githubPATSecretName = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Preprovisioned controller-only Kubernetes Secret containing a GitHub token key.";
    };
    gitlabPATSecretName = lib.mkOption { type = lib.types.str; default = ""; description = "Controller-only GitLab PAT Secret, when owner vault mode is disabled."; };
    gitlabAPIURL = lib.mkOption { type = lib.types.str; default = "https://gitlab.com/api/v4"; description = "Operator-approved GitLab REST endpoint."; };
    vault = {
      enable = lib.mkEnableOption "direct service credential reads from independent owner vault";
      endpoint = lib.mkOption { type = lib.types.str; default = ""; description = "Exact private vault HTTPS origin."; };
      serverName = lib.mkOption { type = lib.types.str; default = ""; description = "Pinned owner vault TLS identity."; };
      caSecretName = lib.mkOption { type = lib.types.str; default = ""; description = "Kubernetes Secret containing trust roots."; };
      clientIdentitySecretName = lib.mkOption { type = lib.types.str; default = ""; description = "Dedicated controller mTLS identity Secret."; };
      githubAlias = lib.mkOption { type = lib.types.str; default = ""; };
      gitlabAlias = lib.mkOption { type = lib.types.str; default = ""; };
      embeddingsAlias = lib.mkOption { type = lib.types.str; default = ""; };
    };
    environmentRegistry = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Optional approved HTTPS registry and repository prefix for environment images.";
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
      jwksURL = lib.mkOption {
        type = lib.types.str;
        default = "";
      };
      ownerEmailSecretName = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Secret with the authorized Cloudflare Access owner email.";
      };
    };
    embedding = {
      url = lib.mkOption { type = lib.types.str; default = ""; description = "HTTPS or loopback embeddings-compatible endpoint."; };
      model = lib.mkOption { type = lib.types.str; default = ""; description = "Configured embedding model identifier."; };
      tokenSecretName = lib.mkOption { type = lib.types.str; default = ""; description = "Preprovisioned controller-only Secret with token key."; };
    };
    backupInterval = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Restic snapshot interval (for example 24h); empty disables the controller scheduler.";
    };
    offsite = {
      repository = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Restic repository URL for encrypted, external controller and artifact backups.";
      };
      passwordSecretName = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Preprovisioned Kubernetes Secret with the Restic password key.";
      };
      backendSecretName = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Optional Kubernetes Secret with storage transport credentials.";
      };
      sshSecretName = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Preprovisioned backup-scoped SSH key and pinned known_hosts for SFTP Restic repositories.";
      };
    };
    publicURL = lib.mkOption {
      type = lib.types.str;
      default = "";
    };
    allowedHosts = lib.mkOption {
      type = lib.types.str;
      default = "pwm-controller,pwm-controller.pwm-system.svc.cluster.local";
    };
    allowedOrigins = lib.mkOption {
      type = lib.types.str;
      default = "https://pwm-controller.pwm-system.svc.cluster.local";
    };
    workers = lib.mkOption {
      type = lib.types.attrsOf workerType;
      default = {};
      description = "Explicitly enrolled, namespace-scoped project workers.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = (cfg.embedding.url == "") == (cfg.embedding.model == "")
          && (cfg.embedding.url != "" || (cfg.embedding.tokenSecretName == "" && cfg.vault.embeddingsAlias == ""));
        message = "Embedding configuration requires a model and exact provider endpoint.";
      }
      {
        assertion = !cfg.vault.enable || (cfg.vault.endpoint != "" && cfg.vault.serverName != "" &&
          cfg.vault.caSecretName != "" && cfg.vault.clientIdentitySecretName != "" &&
          (cfg.vault.githubAlias != "" || cfg.vault.gitlabAlias != "" || cfg.vault.embeddingsAlias != ""));
        message = "Compute vault requires pinned mTLS trust and at least one credential alias.";
      }
      {
        assertion = cfg.vault.enable ||
          (cfg.vault.githubAlias == "" && cfg.vault.gitlabAlias == "" && cfg.vault.embeddingsAlias == "");
        message = "Compute credential aliases require owner vault access.";
      }
      {
        assertion = (cfg.vault.githubAlias == "" || cfg.githubPATSecretName == "") &&
          (cfg.vault.gitlabAlias == "" || cfg.gitlabPATSecretName == "") &&
          (cfg.vault.embeddingsAlias == "" || cfg.embedding.tokenSecretName == "");
        message = "Compute providers require one credential authority.";
      }
      {
        assertion = imageIsPinned cfg.controllerImage;
        message = "Workspace requires a digest-pinned controller image.";
      }
      {
        assertion = cfg.workers == {} || imageIsPinned cfg.workerImage;
        message = "Enabled Workspace workers require a digest-pinned worker image.";
      }
      {
        assertion = lib.all (worker:
          worker.builder.image == "" ||
            (imageIsPinned worker.builder.image &&
              worker.builder.registry != "" &&
              worker.builder.registry == cfg.environmentRegistry &&
              worker.builder.authSecretName != "")
        ) (builtins.attrValues cfg.workers);
        message = "Workspace builders require a digest-pinned image, a credential Secret and the controller's OCI registry prefix.";
      }
      {
        assertion = cfg.backupInterval == "" ||
          (cfg.offsite.repository != "" && cfg.offsite.passwordSecretName != "");
        message = "Workspace scheduled backups require a Restic repository and an external password Secret.";
      }
      {
        assertion = !(lib.hasPrefix "sftp:" cfg.offsite.repository) || cfg.offsite.sshSecretName != "";
        message = "Workspace SFTP backups require an SSH Secret with a scoped identity and pinned known_hosts.";
      }
    ];

    systemd.services = {
      workspace-controller = {
      description = "Deploy Workspace Kubernetes controller";
      wantedBy = [ "multi-user.target" ];
      requires = [ "k3s.service" ];
      after = [ "k3s.service" ];
      path = [ pkgs.k3s pkgs.kubectl pkgs.kubernetes-helm ];
      environment.KUBECONFIG = "/etc/rancher/k3s/k3s.yaml";
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        TimeoutStartSec = "15min";
      };
      script = ''
        set -euo pipefail
        kubectl get namespace pwm-system >/dev/null 2>&1 || kubectl create namespace pwm-system
        kubectl label namespace pwm-system pod-security.kubernetes.io/enforce=restricted --overwrite
        kubectl -n pwm-system get secret pwm-controller-credentials >/dev/null
        kubectl -n pwm-system get secret pwm-controller-tls >/dev/null
        ${lib.optionalString (cfg.githubPATSecretName != "") ''
          kubectl -n pwm-system get secret ${lib.escapeShellArg cfg.githubPATSecretName} >/dev/null
        ''}
        ${lib.optionalString (cfg.access.ownerEmailSecretName != "") ''
          kubectl -n pwm-system get secret ${lib.escapeShellArg cfg.access.ownerEmailSecretName} >/dev/null
        ''}
        ${lib.optionalString (cfg.gitlabPATSecretName != "") ''
          kubectl -n pwm-system get secret ${lib.escapeShellArg cfg.gitlabPATSecretName} >/dev/null
        ''}
        ${lib.optionalString cfg.vault.enable ''
          kubectl -n pwm-system get secret ${lib.escapeShellArg cfg.vault.caSecretName} >/dev/null
          kubectl -n pwm-system get secret ${lib.escapeShellArg cfg.vault.clientIdentitySecretName} >/dev/null
        ''}
        ${lib.optionalString (cfg.embedding.tokenSecretName != "") ''
          kubectl -n pwm-system get secret ${lib.escapeShellArg cfg.embedding.tokenSecretName} >/dev/null
        ''}
        helm upgrade --install pwm-controller ${lib.escapeShellArg chartPath} \
          --namespace pwm-system --values ${controllerValues} \
          --wait --timeout 10m
      '';
    };

    } // (lib.mapAttrs' (name: worker:
      lib.nameValuePair "workspace-worker-${name}" {
        description = "Deploy Workspace worker ${worker.projectID}";
        wantedBy = [ "multi-user.target" ];
        requires = [ "workspace-controller.service" ];
        after = [ "workspace-controller.service" ];
        path = [ pkgs.k3s pkgs.kubectl pkgs.kubernetes-helm ];
        environment.KUBECONFIG = "/etc/rancher/k3s/k3s.yaml";
        serviceConfig = {
          Type = "oneshot";
          RemainAfterExit = true;
          TimeoutStartSec = "15min";
        };
        script = ''
          set -euo pipefail
          kubectl -n pwm-system get secret ${lib.escapeShellArg worker.tokenSecretName} >/dev/null
          kubectl -n pwm-system get secret pwm-controller-ca >/dev/null
          helm upgrade --install ${lib.escapeShellArg "pwm-${name}"} ${lib.escapeShellArg chartPath} \
            --namespace pwm-system --values ${workerValues name worker} \
            --wait --timeout 10m
        '';
      }) cfg.workers);
  };
}
