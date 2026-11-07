{ config, lib, pkgs, ... }:
let
  cfg = config.homelab.native.applications;
  compose = "${pkgs.docker-compose}/bin/docker-compose";
  stack = name: directory: args: {
    description = "Saturn ${name} application containers";
    wantedBy = [ "multi-user.target" ];
    after = [ "docker.service" "network-online.target" ];
    wants = [ "network-online.target" ];
    requires = [ "docker.service" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      WorkingDirectory = directory;
      ExecStart = "${compose} ${args} up -d --no-recreate --pull never";
      ExecStop = "${compose} ${args} stop";
      TimeoutStartSec = "15min";
      TimeoutStopSec = "3min";
    };
  };
in {
  options.homelab.native.applications = {
    recipesage.enable = lib.mkEnableOption "RecipeSage";
    seerr.enable = lib.mkEnableOption "Seerr media requests";
    dispatcharr.enable = lib.mkEnableOption "Dispatcharr";
    monitoring.enable = lib.mkEnableOption "Monitoring containers";
    media.enable = lib.mkEnableOption "Jellyfin media server";
    pihole.enable = lib.mkEnableOption "Pi-hole DNS";
  };
  config = {
    virtualisation.docker.enable = lib.mkIf (
      cfg.recipesage.enable || cfg.dispatcharr.enable ||
      cfg.monitoring.enable || cfg.media.enable ||
      cfg.pihole.enable || cfg.seerr.enable
    ) true;

    systemd.services.saturn-recipesage = lib.mkIf cfg.recipesage.enable
      (stack "RecipeSage" "/srv/homelab/stacks/recipesage"
        "--env-file /srv/homelab/secrets/recipesage.env -f compose.yaml -f override.yaml");


    systemd.services.saturn-seerr = lib.mkIf cfg.seerr.enable
      ((stack "Seerr" "/etc/nixos/homelab/stacks/seerr" "-f compose.yaml") // {
        after = [ "docker.service" "network-online.target" "saturn-media.service" ];
        wants = [ "network-online.target" "saturn-media.service" ];
        preStart = ''
          ${pkgs.coreutils}/bin/install -d -o 1000 -g 1000 -m 0750 /srv/homelab/data/seerr/config
        '';
      });

    systemd.services.saturn-monitoring = lib.mkIf cfg.monitoring.enable
      (lib.recursiveUpdate
        (stack "monitoring" "/srv/homelab/stacks/monitoring"
          "--profile logs --profile storage-health -f compose.yaml")
        {
          # The Compose manifest is tracked in Git; service credentials and
          # site-specific alert configuration remain under /srv/homelab.
          preStart = ''
            ${pkgs.coreutils}/bin/install -d -m 0755 /srv/homelab/stacks/monitoring
            ${pkgs.coreutils}/bin/install -m 0644 /etc/nixos/homelab/stacks/monitoring/compose.yaml /srv/homelab/stacks/monitoring/compose.yaml
          '';
          serviceConfig.ExecStart =
            "${compose} --profile logs --profile storage-health -f compose.yaml up -d --pull never";
        });

    systemd.services.saturn-media = lib.mkIf cfg.media.enable
      (stack "media" "/srv/homelab/stacks/media" "-f compose.yaml");


    systemd.services.saturn-pihole = lib.mkIf cfg.pihole.enable
      (stack "Pi-hole" "/srv/homelab/stacks/pihole" "-f ${pkgs.writeText "saturn-pihole-compose.yaml" (builtins.readFile ./stacks/pihole/compose.yaml)}");


    assertions = (lib.optionals cfg.dispatcharr.enable [{
      assertion = config.homelab.native.downloads.enable;
      message = "Dispatcharr requires the verified VPN infrastructure.";
    }]) ++ (lib.optionals cfg.seerr.enable [{
      assertion = cfg.media.enable;
      message = "Seerr needs the Saturn Jellyfin media network.";
    }]);
  };
}
