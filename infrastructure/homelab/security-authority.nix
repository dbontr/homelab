{ ... }:
let
  secretDir = "/srv/homelab/secrets/security";
  host = "saturn";
  privilegedOperations = [
    "infrastructure__service_start"
    "infrastructure__service_stop"
    "infrastructure__service_restart"
    "infrastructure__service_reload"
    "infrastructure__workspace_create"
    "infrastructure__workspace_stop"
    "infrastructure__workspace_register_image"
    "infrastructure__workspace_register_software"
    "infrastructure__workspace_build_environment"
    "infrastructure__workspace_propose_program"
    "infrastructure__workspace_review_program"
    "infrastructure__workspace_update_entry"
    "infrastructure__workspace_revoke_worker"
    "cluster__deployment_restart"
    "cluster__deployment_scale"
    "infrastructure__release_stage"
    "infrastructure__release_activate"
    "infrastructure__release_revert"
  ];
in
{
  services.homelabSecurity = {
    enable = true;
    implementation = "rust";
    migration.receiptFile = "${secretDir}/authority-rust-cutover.json";
    owner = "owner";
    passwordVerifierFile = "${secretDir}/owner-verifier.json";
    totpSecretFile = "${secretDir}/owner-totp";
    vaultKeyFile = "${secretDir}/vault.key";
    access = {
      issuer = "https://dbontr.cloudflareaccess.com";
      gatewayAudience = "85181fd33a10983cc64020e9155ffeeea8f9e4c6b7af6dd1a51f5c3588f63f25";
      ownerEmailFile = "/srv/homelab/secrets/adaptive-access-owner.email";
      jwksFile = "${secretDir}/access-jwks.json";
    };
    browser = {
      enable = true;
      hostname = "auth.dbontr.me";
      audience = "51fb5fbe71e4a1e0a110630c3f1325c5e3567927538ee697ff91c4e3165972dd";
    };
    policies = (map (operation: {
      inherit operation;
      targets = [ host ];
      destructive = true;
    }) privilegedOperations) ++ (map (operation: {
      inherit operation;
      targets = [ host ];
      destructive = false;
    }) [
      "compute__submit_batch_job"
      "compute__cancel_batch_job"
      "infrastructure__release_status"
    ]);
  };

  services.homelabExecutor = {
    enable = true;
    implementation = "rust";
    useRustActuator = true;
    allowReleaseActivation = true;
    sourceDirectory = "/srv/homelab-main-build";
    workspace = {
      enable = true;
      endpoint = "http://127.0.0.1:8104/workspace";
      rootTokenFile = "/srv/homelab/secrets/compute-controller-admin.token";
      projects = [
        "acceptance"
        "aup-zu3-pynq"
        "health-stack"
        "homelab-platform"
        "minecraft-rust"
        "web-game-console-emulator"
        "workspace-mcp"
      ];
      imageReferences = [
        "docker.io/library/debian@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3"
      ];
    };
    allowedServices = [ "homelab-motivation.service" ];
    healthServices = [
      "homelab-platform.service"
      "homelab-mcp.service"
      "homelab-api.service"
      "homelab-securityd.service"
    ];
  };
}
