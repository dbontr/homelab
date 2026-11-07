{ config, lib, pkgs, ... }:
{
  options.homelab.backups.enable = lib.mkEnableOption "scheduled Saturn recovery backups";

  config = lib.mkIf config.homelab.backups.enable {
  systemd.services.saturn-backup = {
    description = "Saturn configuration and database backup";
    requires = [ "docker.service" ];
    after = [ "docker.service" ];
    path = with pkgs; [ docker gnutar gzip coreutils sqlite findutils gnugrep systemd util-linux jq ];
    serviceConfig = {
      Type = "oneshot";
      UMask = "0077";
    };
    script = ''
      set -euo pipefail
      stamp="$(date -u +%Y%m%dT%H%M%SZ)"
      dest="/srv/homelab/backups/$stamp"
      mkdir -p "$dest"
      chmod 0700 "$dest"
      tar -C /etc -czf "$dest/nixos-config.tar.gz" nixos
      if [ -d /root/portunus-switch-backups ]; then
        tar -C /root -czf "$dest/portunus-switch-config.tar.gz" portunus-switch-backups
        tar -tzf "$dest/portunus-switch-config.tar.gz" > /dev/null
        printf 'Portunus switch snapshots included.\n' > "$dest/portunus-status.txt"
      else
        printf 'Portunus switch snapshots missing.\n' > "$dest/portunus-status.txt"
      fi
      if [ -f /var/lib/tailscale-media/tailscaled.state ]; then
        tar -C /var/lib -czf "$dest/tailscale-media-state.tar.gz" tailscale-media/tailscaled.state
        tar -C /var/lib -czf "$dest/tailscale-media.tar.gz" tailscale-media
        tar -tzf "$dest/tailscale-media.tar.gz" > /dev/null
        tar -tzf "$dest/tailscale-media-state.tar.gz" | grep -Fxq "tailscale-media/tailscaled.state"
      else
        printf "Media tailnet identity is not enrolled.\n" > "$dest/tailscale-media-status.txt"
      fi
      tar -C /srv/homelab -czf "$dest/application-config.tar.gz" stacks secrets
      docker exec recipesage_postgres pg_dumpall -U recipesage_selfhost |
        gzip -c > "$dest/recipesage-pg.sql.gz"
      gzip -t "$dest/recipesage-pg.sql.gz"
      sqlite3 /var/lib/rancher/k3s/server/db/state.db ".backup $dest/k3s-state.db"
      test "$(sqlite3 "$dest/k3s-state.db" "PRAGMA integrity_check;")" = "ok"
      install -m 0600 /var/lib/rancher/k3s/server/token "$dest/k3s-server-token"
      sqlite3 /srv/homelab/data/seerr/config/db/db.sqlite3 ".backup $dest/seerr-db.sqlite3"
      chown --reference=/srv/homelab/data/seerr/config/db/db.sqlite3 "$dest/seerr-db.sqlite3"
      chmod --reference=/srv/homelab/data/seerr/config/db/db.sqlite3 "$dest/seerr-db.sqlite3"
      test "$(sqlite3 "$dest/seerr-db.sqlite3" "PRAGMA integrity_check;")" = "ok"
      tar -C /srv/homelab/data/seerr/config --exclude='./db' --exclude='./logs' -czf "$dest/seerr-state.tar.gz" .
      tar -C /srv -czf "$dest/project-knowledge.tar.gz" project-knowledge
      tar --exclude="*/.checkout-*" -C /srv -czf "$dest/project-source.tar.gz" project-source/repos project-source/worktrees
      # Capture consistent SQLite snapshots before archiving the course artifacts.
      bash ${./knowledge-index-backup.sh} snapshot /srv/artifact-library/knowledge-index "$dest/knowledge-index-catalogs"
      tar -C "$dest" -czf "$dest/knowledge-index-catalogs.tar.gz" knowledge-index-catalogs
      # Hold the task manifest lock while capturing a consistent artifact index.
      flock -x /srv/artifact-library/.tasks.lock \
        tar -C /srv -czf "$dest/artifact-library.tar.gz" artifact-library
      tar -C /srv -czf "$dest/project-artifacts.tar.gz" project-source/artifacts
      restore="$(mktemp -d "$dest/.restore.XXXXXX")"
      trap 'rm -rf -- "$restore"' EXIT
      tar -xzf "$dest/artifact-library.tar.gz" -C "$restore"
      tar -xzf "$dest/project-artifacts.tar.gz" -C "$restore"
      bash ${./knowledge-index-backup.sh} verify-restore "$dest/knowledge-index-catalogs" "$restore/artifact-library/knowledge-index"
      bash ${./verify-artifact-backup.sh} \
        "$restore/artifact-library" "$restore/project-source/artifacts"
      rm -rf -- "$restore"
      trap - EXIT
      tar -tzf "$dest/project-knowledge.tar.gz" > /dev/null
      tar -tzf "$dest/project-source.tar.gz" > /dev/null
      tar -tzf "$dest/nixos-config.tar.gz" > /dev/null
      tar -tzf "$dest/application-config.tar.gz" > /dev/null
      (
        cd "$dest"
        inputs=(nixos-config.tar.gz application-config.tar.gz recipesage-pg.sql.gz k3s-state.db k3s-server-token seerr-db.sqlite3 seerr-state.tar.gz project-knowledge.tar.gz project-source.tar.gz artifact-library.tar.gz project-artifacts.tar.gz knowledge-index-catalogs.tar.gz)
        if [ -f tailscale-media-state.tar.gz ]; then inputs+=(tailscale-media-state.tar.gz); else inputs+=(tailscale-media-status.txt); fi
        if [ -f tailscale-media.tar.gz ]; then inputs+=(tailscale-media.tar.gz); fi
        inputs+=(portunus-status.txt)
        if [ -f portunus-switch-config.tar.gz ]; then inputs+=(portunus-switch-config.tar.gz); fi
        sha256sum "''${inputs[@]}" > SHA256SUMS
        sha256sum -c SHA256SUMS
      )
      touch "$dest/complete"
    '';
  };
  systemd.services.saturn-offhost-backup = {
    description = "Copy encrypted Saturn recovery archives to Janus";
    wants = [ "network-online.target" ];
    after = [ "network-online.target" ];
    path = with pkgs; [ bash age openssh gnutar coreutils ];
    serviceConfig = {
      Type = "oneshot";
      UMask = "0077";
      Nice = 15;
      TimeoutStartSec = "2h";
    };
    script = ''
      exec ${pkgs.bash}/bin/bash ${./saturn-offhost-backup.sh}
    '';
  };
  systemd.timers.saturn-offhost-backup = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "hourly";
      Persistent = true;
      RandomizedDelaySec = "15min";
    };
  };

  systemd.timers.saturn-backup = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "daily";
      Persistent = true;
      RandomizedDelaySec = "30min";
    };
  };
  };
}
