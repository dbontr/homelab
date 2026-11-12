{ config, lib, pkgs, ... }:
let
  cfg = config.homelab.native.kubernetes;
in
{
  options.homelab.native.kubernetes.enable =
    lib.mkEnableOption "Saturn K3s control plane on the existing LAN";
  config = lib.mkIf cfg.enable {
    services.k3s = {
      enable = true;
      role = "server";
      nodeIP = "10.77.30.1";
      extraFlags = [
        "--disable=traefik"
        "--disable=servicelb"
        "--flannel-iface=compute30"
        "--tls-san=10.77.30.1"
        "--embedded-registry"
        "--kube-apiserver-arg=enable-admission-plugins=NodeRestriction"
      ];
    };
    networking.firewall.interfaces.enp0s31f6.allowedTCPPorts = [ 5001 6443 10250 ];
    networking.firewall.interfaces.enp0s31f6.allowedUDPPorts = [ 8472 ];
    networking.firewall.trustedInterfaces = [ "cni0" "flannel.1" ];
    environment.etc."rancher/k3s/registries.yaml".text = ''
      mirrors:
        "images.dbontr.internal":
        "ghcr.io":
    '';

    systemd.services.k3s-compute-policy = {
      description = "Register the NVIDIA CDI RuntimeClass";
      wantedBy = [ "multi-user.target" ];
      requires = [ "k3s.service" ];
      after = [ "k3s.service" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        for attempt in $(seq 1 30); do
          if ${config.services.k3s.package}/bin/k3s kubectl get --raw=/readyz >/dev/null 2>&1; then
            ${config.services.k3s.package}/bin/k3s kubectl apply -f ${./nvidia-runtimeclass.yaml}
            exit 0
          fi
          sleep 2
        done
        echo "K3s API was not ready for compute policy application" >&2
        exit 1
      '';
    };
  };
}
