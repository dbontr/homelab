{
  description = "Self-hosted personal computing platform";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/da6dc0e7b26060f2f71ce3ef7088fb56ee44fa36";

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      worker = module: nixpkgs.lib.nixosSystem {
        inherit system;
        modules = [ module ];
      };
    in {
      nixosModules.clusterAgent = import ./infrastructure/modules/cluster-agent.nix;
      nixosModules.homelabPlatform = import ./nixosModules/platform.nix;
      packages.${system}.homelab-platform = import ./packages/platform.nix {
        pkgs = nixpkgs.legacyPackages.${system};
      };

      nixosConfigurations = {
        saturn = nixpkgs.lib.nixosSystem {
          inherit system;
          specialArgs.workspaceMcp = ./system/compute;
          modules = [ ./infrastructure/hosts/saturn/configuration.nix ];
        };
        jupiter = worker ./infrastructure/hosts/jupiter/configuration.nix;
        venus = worker ./infrastructure/hosts/venus/configuration.nix;
        mercury = worker ./infrastructure/hosts/mercury/configuration.nix;
        janus = worker ./infrastructure/hosts/janus/configuration.nix;
      };
    };
}
