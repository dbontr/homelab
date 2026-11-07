{ lib, ... }: {
  homelab.router.enable = true;
  homelab.router.isolationVerified = true;
  systemd.network.networks."20-lan-trunk".address = [ "192.168.2.250/24" ];
  services.k3s.nodeIP = lib.mkForce "10.77.30.1";
  services.k3s.extraFlags = lib.mkForce [ "--disable=traefik" "--disable=servicelb" "--flannel-iface=compute30" "--tls-san=10.77.30.1" "--embedded-registry" "--kube-apiserver-arg=enable-admission-plugins=NodeRestriction" ];
  networking.firewall.interfaces.compute30.allowedTCPPorts = [ 6443 10250 5001 ];
  networking.firewall.interfaces.compute30.allowedUDPPorts = [ 8472 ];
  networking.firewall.interfaces.services20.allowedTCPPorts = [ 80 ];
  networking.firewall.interfaces.management10.allowedTCPPorts = [ 80 ];
  services.dnsmasq.settings.address = [
    "/recipes.home.arpa/10.77.20.1"
    "/frappe.home.arpa/10.77.20.1"
  ];
  services.resolved.enable = true;
}
