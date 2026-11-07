{ config, lib, pkgs, ... }:
{
  services.openssh.enable = true;
  services.openssh.settings.KbdInteractiveAuthentication = false;
  users.users.root.openssh.authorizedKeys.keyFiles = [ ./saturn-homelab.pub ];
  services.openssh.settings.PasswordAuthentication = false;
  services.openssh.settings.PermitRootLogin = "prohibit-password";
  services.prometheus.exporters.node = {
    enable = true; port = 9100; openFirewall = false;
  };
  networking.firewall.enable = true;
  networking.firewall.interfaces.enp0s31f6 = {
    allowedTCPPorts = [ 22 5001 6443 9100 10250 ];
    allowedUDPPorts = [ 8472 ];
  };
  environment.etc."rancher/k3s/registries.yaml".text = ''
    mirrors:
      "images.dbontr.internal":
  '';
  services.k3s = {
    enable = true;
    role = "agent";
    serverAddr = "https://10.77.30.1:6443";
    tokenFile = "/var/lib/secrets/k3s-agent-token";
    nodeIP = "10.77.30.232";
    extraFlags = [
      "--node-label=homelab-role=compute"
      "--flannel-iface=enp0s31f6"
    ];
  };
}
