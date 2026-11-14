{ workspaceMcp, pkgs, ... }:
{
  imports = [
    ../../nixosModules/platform.nix
    ../../nixosModules/security.nix
    ../../nixosModules/executor.nix
    ../../nixosModules/console.nix
    ../../nixosModules/interfaces.nix
    ./shared.nix
    ./security-authority.nix
    ./domain-services.nix
    ./cloudflare.nix
    ./mcp-ingress.nix
    ./orchestration.nix
    ./recovery.nix
    ./exporters.nix
    ./gpu-telemetry.nix
    ./media-tailnet.nix
    ./media-local.nix
    ./media-proxy.nix
    ./downloads.nix
    ./adaptive-teaching.nix
    (workspaceMcp + "/deploy/nixos/module.nix")
    (workspaceMcp + "/deploy/nixos/project-provisioner.nix")
    ./recipes-proxy.nix
    ./personal-health.nix
  ];

  homelab.native.applications.pihole.enable = true;
  homelab.native.kubernetes.enable = true;

  homelab.native.exporters.enable = true;
  homelab.native.applications.monitoring.enable = true;
  services.homelabPlatform.monitoring = {
    prometheusURL = "http://127.0.0.1:9090";
    lokiURL = "http://127.0.0.1:3100";
  };

  homelab.native.downloads.enable = true;
  homelab.native.applications.media.enable = true;
  homelab.native.applications.seerr.enable = true;
  homelab.native.applications.dispatcharr.enable = false;

  homelab.learning = {
    enable = true;
    package = import ../../packages/learning.nix { inherit pkgs; };
    bearerCredentialFile = "/srv/homelab/secrets/adaptive-teaching.token";
    listenAddress = "127.0.0.1:8200";
    allowedHosts = "127.0.0.1:*,localhost:*";
    allowedOrigins = "https://chatgpt.com,https://chat.openai.com";
    access = {
      issuer = "https://dbontr.cloudflareaccess.com";
      audience = "85181fd33a10983cc64020e9155ffeeea8f9e4c6b7af6dd1a51f5c3588f63f25";
      certsURL = "https://dbontr.cloudflareaccess.com/cdn-cgi/access/certs";
      ownerCredentialFile = "/srv/homelab/secrets/adaptive-access-owner.email";
    };
  };

  services.workspaceMcpProjectProvisioner = {
    enable = true;
    workerImage = "ghcr.io/dbontr/homelab/worker@sha256:c7ab035296db9aec360611c52737ca4e94143fb959c7ad56ca536cc20fd6ee4f";
    computeNodes = [ "jupiter" "venus" "mercury" "janus" ];
    excludeProjects = [ "acceptance" ];
    interval = "2min";
  };

  services.workspaceMcp = {
    controllerNode = "saturn";
    publicURL = "https://mcp.dbontr.me/workspace";
    githubPATSecretName = "pwm-controller-github-pat";
    access = {
      issuer = "https://dbontr.cloudflareaccess.com";
      audience = "85181fd33a10983cc64020e9155ffeeea8f9e4c6b7af6dd1a51f5c3588f63f25";
      jwksURL = "https://dbontr.cloudflareaccess.com/cdn-cgi/access/certs";
      ownerEmailSecretName = "pwm-controller-cloudflare-owner";
    };
  };

  # The first-party Recipes page supersedes the retired RecipeSage runtime.
  homelab.native.applications.recipesage.enable = false;

  homelab.personalHealth = {
    enable = true;
    package = import ../../packages/health.nix { inherit pkgs; };
    access = {
      issuer = "https://dbontr.cloudflareaccess.com";
      audience = "85181fd33a10983cc64020e9155ffeeea8f9e4c6b7af6dd1a51f5c3588f63f25";
      certsURL = "https://dbontr.cloudflareaccess.com/cdn-cgi/access/certs";
      ownerCredentialFile = "/srv/homelab/secrets/adaptive-access-owner.email";
    };
  };
}
