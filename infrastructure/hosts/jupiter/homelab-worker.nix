{ hardwareNode, ... }:
{
  imports = [
    (import ../../modules/compute-worker.nix {
      node = hardwareNode;
      keyFile = ./saturn-homelab.pub;
    })
  ];
}
