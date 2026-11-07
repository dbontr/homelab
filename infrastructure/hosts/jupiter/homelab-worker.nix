{ lib, ... }: {
  imports = [ ../../modules/cluster-agent.nix ];
  networking.hostName = lib.mkForce "jupiter";
  services.openssh.enable = true;
  services.openssh.settings.PasswordAuthentication = false;
  services.openssh.settings.KbdInteractiveAuthentication = false;
  services.openssh.settings.PermitRootLogin = "prohibit-password";
  users.users.root.openssh.authorizedKeys.keyFiles = [ ./saturn-homelab.pub ];
  networking.firewall.interfaces."enp129s0".allowedTCPPorts = [ 22 ];
  homelab.clusterAgent = {
    enable = true;
    nodeIP = "10.77.30.229";
    lanInterface = "enp129s0";
  };
  services.k3s.enable = lib.mkForce true;
}
