{ lib, ... }: {
  networking.nftables.extraDeletions = lib.mkAfter ''
    table ip portunus_admin;
    delete table ip portunus_admin;
  '';
  networking.nftables.ruleset = lib.mkAfter ''
    table ip portunus_admin {
      chain postrouting {
        type nat hook postrouting priority srcnat; policy accept;
        oifname "enp0s31f6" ip saddr 10.77.10.0/24 ip daddr 192.168.2.1 snat to 192.168.2.250
      }
    }
  '';
}
