{ config, lib, pkgs, ... }:
let
  cfg = config.homelab.native.kueue;
  k3s = "${config.services.k3s.package}/bin/k3s";
  manifest = ./kueue/helmchart.yaml;
  queues = ./kueue/queues.yaml;
  verifier = ../../scripts/verify-kueue.sh;
in
{
  options.homelab.native.kueue.enable =
    lib.mkEnableOption "Kueue admission for explicitly queued Kubernetes batch Jobs";

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = config.homelab.native.kubernetes.enable;
        message = "Kueue requires the Saturn K3s control plane.";
      }
    ];

    systemd.services.homelab-kueue = {
      description = "Reconcile Kueue controller and opt-in compute queues";
      wantedBy = [ "multi-user.target" ];
      requires = [ "k3s.service" ];
      after = [ "k3s.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      path = with pkgs; [ bash coreutils jq gnugrep ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        TimeoutStartSec = "20min";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
      };
      script = ''
        export KUBECTL_BIN="${k3s}"
        for attempt in $(seq 1 90); do
          if ${k3s} kubectl get --raw=/readyz >/dev/null 2>&1; then
            break
          fi
          sleep 2
        done
        ${k3s} kubectl get --raw=/readyz >/dev/null
        ${k3s} kubectl apply --server-side -f ${manifest}

        available=0
        for attempt in $(seq 1 90); do
          if ${k3s} kubectl -n kueue-system get deployment kueue-controller-manager >/dev/null 2>&1; then
            available=1
            break
          fi
          sleep 5
        done
        if [ "$available" -ne 1 ]; then
          echo "Kueue installation did not create the controller Deployment" >&2
          exit 1
        fi
        ${k3s} kubectl -n kueue-system rollout status deployment/kueue-controller-manager --timeout=600s
        ${k3s} kubectl apply --server-side -f ${queues}

        for attempt in $(seq 1 30); do
          if ${pkgs.bash}/bin/bash ${verifier} --check >/dev/null 2>&1; then
            exit 0
          fi
          sleep 5
        done
        ${pkgs.bash}/bin/bash ${verifier} --check
      '';
    };
  };
}
