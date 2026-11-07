{ node, keyFile }:
{ lib, ... }:
{
  imports = [ ./cluster-agent.nix ];

  networking.hostName = lib.mkForce node.id;
  services.openssh.enable = true;
  services.openssh.settings = {
    PasswordAuthentication = false;
    KbdInteractiveAuthentication = false;
    PermitRootLogin = "prohibit-password";
  };
  users.users.root.openssh.authorizedKeys.keyFiles = [ keyFile ];
  networking.firewall.interfaces.${node.lanInterface}.allowedTCPPorts = [ 22 ];

  homelab.clusterAgent = {
    enable = true;
    nodeIP = node.nodeIP;
    lanInterface = node.lanInterface;
  };
  services.k3s.enable = lib.mkForce true;
}
