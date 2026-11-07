{ ... }:
{
  imports = [
    ./backups.nix
    ./selected-backup.nix
  ];

  homelab.backups.enable = false;
  homelab.recovery.selected.enable = false;
}
