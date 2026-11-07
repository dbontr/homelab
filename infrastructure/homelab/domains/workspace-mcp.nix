{ workspaceMcp, ... }:
{
  imports = [
    (workspaceMcp + "/deploy/nixos/module.nix")
    (workspaceMcp + "/deploy/nixos/project-provisioner.nix")
  ];

  services.workspaceMcpProjectProvisioner = {
    enable = true;
    workerImage = "docker.io/library/project-workspace-worker:cuda-exec-756b88c";
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
}
