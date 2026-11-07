{ lib, ... }: {
  networking.hostName = lib.mkForce "mercury";
  services.openssh.enable = true;
  services.openssh.openFirewall = false;
  services.openssh.settings.PasswordAuthentication = false;
  services.openssh.settings.KbdInteractiveAuthentication = false;
  services.openssh.settings.PermitRootLogin = "prohibit-password";
  users.users.root.openssh.authorizedKeys.keyFiles = [ ./saturn-homelab.pub ];
  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [ 22 ];
}
