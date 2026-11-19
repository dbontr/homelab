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
    shell = "${pkgs.util-linux}/bin/nologin";
  };

  systemd.tmpfiles.rules = [
    "d /srv/compute-restic 0755 root root -"
    "d /srv/compute-restic/repository 0700 compute-backup compute-backup -"
    "d /etc/ssh/authorized_keys.d 0755 root root -"
    # The dedicated account must be able to read this root-owned key list.\n    # Root retains exclusive write custody; no world access or shell access.
    "f /etc/ssh/authorized_keys.d/compute-backup 0640 root compute-backup -"
  ];

  services.openssh.settings.PermitUserEnvironment = "no";

  services.openssh.extraConfig = ''
    Match User compute-backup
      AuthorizedKeysFile /etc/ssh/authorized_keys.d/compute-backup
      ChrootDirectory /srv/compute-restic
      ForceCommand internal-sftp -d /repository -u 077
      AuthenticationMethods publickey
      DisableForwarding yes
      PermitUserRC no
      AllowStreamLocalForwarding no
      PermitTTY no
      AllowTcpForwarding no
      AllowAgentForwarding no
      X11Forwarding no
      PermitTunnel no
      PasswordAuthentication no
      KbdInteractiveAuthentication no
  '';
}
