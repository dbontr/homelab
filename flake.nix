{
  description = "Self-hosted personal computing platform";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/da6dc0e7b26060f2f71ce3ef7088fb56ee44fa36";

  outputs = { self, nixpkgs }:
    let
      defaultSystem = "x86_64-linux";
      inventory = import ./hardware.nix;
      enrolled = builtins.filter (node: node.mode == "enrolled") inventory.nodes;
      names = map (node: node.id) enrolled;
      hostConfiguration = node: nixpkgs.lib.nixosSystem {
        system = node.system;
        specialArgs.workspaceMcp = ./system/compute;
        modules = [ (./infrastructure/hosts + ("/" + node.id + "/configuration.nix")) ];
      };
    in
    assert builtins.length names == builtins.length (nixpkgs.lib.unique names);
    {
      nixosModules.clusterAgent = import ./infrastructure/modules/cluster-agent.nix;
      nixosModules.homelabPlatform = import ./nixosModules/platform.nix;
      packages.${defaultSystem}.homelab-platform = import ./packages/platform.nix {
        pkgs = nixpkgs.legacyPackages.${defaultSystem};
      };

      nixosConfigurations = builtins.listToAttrs
        (map (node: nixpkgs.lib.nameValuePair node.id (hostConfiguration node)) enrolled);
    };
}
