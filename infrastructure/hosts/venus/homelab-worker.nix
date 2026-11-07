{ lib, ... }: {
  imports = [ ../../modules/cluster-agent.nix ];
  networking.hostName = lib.mkForce "venus";
  services.openssh.enable = true;
  services.openssh.settings.PasswordAuthentication = false;
  services.openssh.settings.KbdInteractiveAuthentication = false;
  services.openssh.settings.PermitRootLogin = "prohibit-password";
  users.users.root.openssh.authorizedKeys.keyFiles = [ ./saturn-homelab.pub ];
  networking.firewall.interfaces."enp2s0".allowedTCPPorts = [ 22 ];
  homelab.clusterAgent = {
    enable = true;
    nodeIP = "10.77.30.230";
    lanInterface = "enp2s0";
  };
  services.k3s.enable = lib.mkForce true;
}
