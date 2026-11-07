{
  description = "Saturn portable router, application host, and four compute workers";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/da6dc0e7b26060f2f71ce3ef7088fb56ee44fa36";
  inputs.workspaceMcp = { url = "git+ssh://git@github-workspace-mcp/dbontr/workspace-mcp.git?ref=main"; flake = false; };
  outputs = { self, nixpkgs, workspaceMcp }:
    let
      mkWorker = module: nixpkgs.lib.nixosSystem {
        system = "x86_64-linux";
        modules = [ module ];
      };
    in {
      nixosModules.clusterAgent = import ./modules/cluster-agent.nix;
      nixosConfigurations = {
        saturn = nixpkgs.lib.nixosSystem {
          system = "x86_64-linux";
          modules = [ ./hosts/saturn/configuration.nix ];
          specialArgs = { inherit workspaceMcp; };
        };
        jupiter = mkWorker ./hosts/jupiter/configuration.nix;
        venus = mkWorker ./hosts/venus/configuration.nix;
        mercury = mkWorker ./hosts/mercury/configuration.nix;
        janus = mkWorker ./hosts/janus/configuration.nix;
      };
    };
}
