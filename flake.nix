{
  description = "Self-hosted personal computing platform";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/da6dc0e7b26060f2f71ce3ef7088fb56ee44fa36";

  outputs = { self, nixpkgs }:
    let
      defaultSystem = "x86_64-linux";
      inventory = import ./hardware.nix;
      enrolled = builtins.filter (node: node.mode == "enrolled") inventory.nodes;
      names = map (node: node.id) inventory.nodes;
      deviceIDs = map (device: device.id) inventory.devices;
      validNode = node:
        builtins.match "^[a-z][a-z0-9-]*$" node.id != null
        && builtins.elem node.mode [ "ssh" "enrolled" ]
        && (node.mode != "enrolled" || (
          builtins.elem node.role [ "controller" "compute" ]
          && builtins.elem node.system [ "x86_64-linux" "aarch64-linux" ]
          && builtins.pathExists (./infrastructure/hosts + ("/" + node.id + "/configuration.nix"))
          && (node.role != "compute" || (
            node ? nodeIP && node ? lanInterface
            && builtins.match "^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$" node.nodeIP != null
            && node.lanInterface != ""
          ))
        ));
      hostConfiguration = node: nixpkgs.lib.nixosSystem {
        system = node.system;
        specialArgs = { workspaceMcp = ./system/compute; hardwareNode = node; };
        modules = [ (./infrastructure/hosts + ("/" + node.id + "/configuration.nix")) ];
      };
    in
    assert builtins.all validNode inventory.nodes;
    assert builtins.length names == builtins.length (nixpkgs.lib.unique names);
    assert builtins.length deviceIDs == builtins.length (nixpkgs.lib.unique deviceIDs);
    {
      nixosModules.clusterAgent = import ./infrastructure/modules/cluster-agent.nix;
      nixosModules.homelabPlatform = import ./nixosModules/platform.nix;
      nixosModules.homelabEngineering = import ./nixosModules/engineering.nix;
      nixosModules.homelabSecurity = import ./nixosModules/security.nix;
      nixosModules.homelabExecutor = import ./nixosModules/executor.nix;
      nixosModules.homelabFleet = import ./nixosModules/fleet.nix;
      packages.${defaultSystem}.homelab-engineering = import ./packages/engineering.nix {
        pkgs = nixpkgs.legacyPackages.${defaultSystem};
      };
      packages.${defaultSystem}.homelab-platform = import ./packages/platform.nix {
        pkgs = nixpkgs.legacyPackages.${defaultSystem};
      };

      packages.${defaultSystem}.homelab-security = import ./packages/security.nix {
        pkgs = nixpkgs.legacyPackages.${defaultSystem};
      };

      packages.${defaultSystem}.homelab-executor = import ./packages/executor.nix {
        pkgs = nixpkgs.legacyPackages.${defaultSystem};
      };

      packages.${defaultSystem}.homelab-fleet = import ./packages/fleet.nix {
        pkgs = nixpkgs.legacyPackages.${defaultSystem};
      };

      nixosConfigurations = builtins.listToAttrs
        (map (node: nixpkgs.lib.nameValuePair node.id (hostConfiguration node)) enrolled);
    };
}
