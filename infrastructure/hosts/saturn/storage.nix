{ pkgs, ... }:
{
  boot.supportedFilesystems = [ "zfs" ];
  # Four JMS578 (152d:0578) SATA bridges showed UAS command aborts and
  # USB resets under ZFS I/O. Use the conventional USB mass-storage driver.
  boot.kernelParams = [ "usb-storage.quirks=152d:0578:u" ];
  boot.zfs.forceImportRoot = false;
  boot.zfs.forceImportAll = false;
  boot.zfs.extraPools = [ "tank_media" ];
  networking.hostId = "ea843dba";
  environment.systemPackages = [ pkgs.zfs ];
  services.zfs.autoScrub.enable = true;
  fileSystems."/srv/homelab/data/media" = {
    device = "tank_media/jellyfin-media";
    fsType = "zfs";
    options = [ "nofail" ];
  };
  fileSystems."/srv/homelab/data/games" = { device = "tank_media/games"; fsType = "zfs"; options = [ "nofail" ]; };
  fileSystems."/srv/homelab/data/downloads" = {
    device = "/srv/homelab/data/media/.downloads";
    fsType = "none";
    options = [ "bind" "nofail" ];
    depends = [ "/srv/homelab/data/media" ];
  };
  systemd.services.saturn-download-media-permissions = {
    description = "Restrict media and downloader storage to authorized service accounts";
    wantedBy = [ "multi-user.target" ];
    before = [ "radarr.service" "sonarr.service" "lidarr.service" "qbittorrent.service" ];
    unitConfig.RequiresMountsFor = [
      "/srv/homelab/data/media"
      "/srv/homelab/data/downloads"
    ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      ExecStart = "${pkgs.writeShellScript "grant-download-media-access" ''
        set -euo pipefail
        ${pkgs.util-linux}/bin/findmnt --kernel --mountpoint /srv/homelab/data/media --types zfs
        ${pkgs.util-linux}/bin/findmnt --kernel --mountpoint /srv/homelab/data/downloads --types zfs
        acl="${pkgs.acl}/bin/setfacl"
        "$acl" -m u:radarr:--x,u:sonarr:--x,u:lidarr:--x,u:qbittorrent:--x /srv/homelab /srv/homelab/data
        "$acl" -m u:radarr:rwx /srv/homelab/data/media/Movies
        "$acl" -d -m u:radarr:rwx /srv/homelab/data/media/Movies
        "$acl" -m u:sonarr:rwx /srv/homelab/data/media/Shows
        "$acl" -d -m u:sonarr:rwx /srv/homelab/data/media/Shows
        "$acl" -m u:lidarr:rwx /srv/homelab/data/media/music
        "$acl" -d -m u:lidarr:rwx /srv/homelab/data/media/music
        "$acl" -m u:qbittorrent:rwx,u:radarr:rwx,u:sonarr:rwx,u:lidarr:rwx /srv/homelab/data/downloads
        "$acl" -d -m u:qbittorrent:rwx,u:radarr:rwx,u:sonarr:rwx,u:lidarr:rwx /srv/homelab/data/downloads
      ''}";
    };
  };
  systemd.services.saturn-media.unitConfig.RequiresMountsFor = [ "/srv/homelab/data/media" "/srv/homelab/data/games" ];
  systemd.services.saturn-media.serviceConfig.ExecStartPre = "${pkgs.util-linux}/bin/findmnt --kernel --mountpoint /srv/homelab/data/media --types zfs";
}
