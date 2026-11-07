{ config, lib, pkgs, ... }:
let cfg = config.homelab.native.exporters;
in {
  options.homelab.native.exporters.enable =
    lib.mkEnableOption "standalone native Blackbox and SNMP exporters";
  config = lib.mkIf cfg.enable {
    systemd.services.saturn-blackbox = {
      description = "Saturn local Blackbox Exporter";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        ExecStart = "${pkgs.prometheus-blackbox-exporter}/bin/blackbox_exporter --config.file=${./blackbox.yml} --web.listen-address=127.0.0.1:9115";
        Restart = "on-failure";
      };
    };
    systemd.services.saturn-snmp = {
      description = "Saturn local SNMP Exporter";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        ExecStart = "${pkgs.prometheus-snmp-exporter}/bin/snmp_exporter --config.file=${./snmp.yml} --web.listen-address=127.0.0.1:9116";
        Restart = "on-failure";
      };
    };
  };
}
