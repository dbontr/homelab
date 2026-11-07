{ pkgs, ... }:
{
  programs.ssh.knownHosts."github.com".publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl";
  programs.ssh.extraConfig = ''
    Host github-workspace-mcp
      HostName github.com
      HostKeyAlias github.com
      User git
      IdentityFile /root/.ssh/saturn-workspace-mcp-ed25519
      IdentitiesOnly yes
      BatchMode yes
      StrictHostKeyChecking yes
    Host github-adaptive-teaching-mcp
      HostName github.com
      HostKeyAlias github.com
      User git
      IdentityFile /root/.ssh/saturn-adaptive-teaching-mcp-ed25519
      IdentitiesOnly yes
      BatchMode yes
      StrictHostKeyChecking yes
    Host github-personal-health-mcp
      HostName github.com
      HostKeyAlias github.com
      User git
      IdentityFile /root/.ssh/saturn-personal-health-mcp-ed25519
      IdentitiesOnly yes
      BatchMode yes
      StrictHostKeyChecking yes
    Host github-quantitative-finance-mcp
      HostName github.com
      HostKeyAlias github.com
      User git
      IdentityFile /root/.ssh/saturn-quantitative-finance-mcp-ed25519
      IdentitiesOnly yes
      BatchMode yes
      StrictHostKeyChecking yes
    Match localuser root originalhost github.com
      IdentityFile /root/.ssh/saturn-homelab-ed25519
      IdentitiesOnly yes
  '';
  systemd.services.saturn-github-sync = {
    description = "Publish committed Saturn configuration to private GitHub";
    path = [ pkgs.git pkgs.openssh ];
    environment.GIT_TERMINAL_PROMPT = "0";
    serviceConfig = {
      Type = "oneshot";
      WorkingDirectory = "/etc/nixos";
      ExecStart = "${pkgs.git}/bin/git -C /etc/nixos push origin refs/heads/main:refs/heads/main";
      TimeoutStartSec = "120s";
      NoNewPrivileges = true;
    };
  };
  systemd.timers.saturn-github-sync = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "daily";
      Persistent = true;
      RandomizedDelaySec = "30m";
    };
  };
}
