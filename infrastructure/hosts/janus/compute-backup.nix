{ pkgs, ... }:
{
  # The SFTP account cannot execute commands or access the host filesystem.
  # Authorization is provisioned outside the immutable Nix store.
  users.groups.compute-backup = {};
  users.users.compute-backup = {
    isSystemUser = true;
    group = "compute-backup";
    home = "/var/empty";
    createHome = false;
    shell = "${pkgs.shadow}/bin/nologin";
  };

  systemd.tmpfiles.rules = [
    "d /srv/compute-restic 0755 root root -"
    "d /srv/compute-restic/repository 0700 compute-backup compute-backup -"
    "d /srv/homelab 0700 root root -"
    "d /srv/homelab/secrets 0700 root root -"
  ];

  services.openssh.extraConfig = ''
    Match User compute-backup
      AuthorizedKeysFile /srv/homelab/secrets/compute-backup.authorized_keys
      ChrootDirectory /srv/compute-restic
      ForceCommand internal-sftp -d /repository
      PermitTTY no
      AllowTcpForwarding no
      AllowAgentForwarding no
      X11Forwarding no
      PermitTunnel no
      PasswordAuthentication no
      KbdInteractiveAuthentication no
  '';
}
