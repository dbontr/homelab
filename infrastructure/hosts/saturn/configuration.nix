{ pkgs, ... }:
{
  imports = [
    ./hardware-configuration.nix
    ./github-sync.nix
    ./management-access.nix
    ./router.nix
    ./router-rollout.nix
    ./portunus-management.nix
    ./storage.nix
    ../../homelab
  ];
  system.stateVersion = "26.05";
  networking.hostName = "saturn";
  networking.networkmanager.enable = false;
  networking.firewall.enable = true;
  services.nginx = {
    enable = true;
    recommendedProxySettings = true;
  };
  services.openssh = {
    enable = true;
    openFirewall = true;
    settings = {
      PermitRootLogin = "prohibit-password";
      PasswordAuthentication = false;
      KbdInteractiveAuthentication = false;
    };
  };
  users.users.root.openssh.authorizedKeys.keys = [
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJKydheFJFdyPoYRYZxefcxVKPmw+ImnB3cklKo/V3tH root@kvm-2e1c"
  ];
  users.users.devon = {
    isNormalUser = true;
    description = "Infrastructure administrator";
    extraGroups = [ "wheel" "networkmanager" ];
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJKydheFJFdyPoYRYZxefcxVKPmw+ImnB3cklKo/V3tH root@kvm-2e1c"
    ];
  };
  time.timeZone = "America/Chicago";
  i18n.defaultLocale = "en_US.UTF-8";
  boot.loader.systemd-boot.enable = true;
  boot.loader.systemd-boot.configurationLimit = 8;
  boot.loader.efi.canTouchEfiVariables = true;
  boot.initrd.systemd.enable = true;
  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  nix.gc = { automatic = true; dates = "weekly"; options = "--delete-older-than 30d"; };
  services.fstrim.enable = true;
  zramSwap.enable = true;
  virtualisation.docker.enable = true;
  services.prometheus.exporters.node = {
    enable = true; port = 9100; openFirewall = false;
  };
  environment.systemPackages = with pkgs; [
    bash curl wget git vim nano tmux htop btop jq xmlstarlet rsync
    pciutils usbutils ethtool smartmontools nvme-cli
    docker-compose k3s kubectl colmena
  ];
}
