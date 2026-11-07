{ config, pkgs, ... }:
{
  imports = [ ./hardware-configuration.nix ./jupiter-gpu.nix ./jupiter-network.nix ./homelab-worker.nix ./eda-build-vm.nix ];
  homelab.edaBuildVm.enable = true;
  homelab.edaBuildVm.controlPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMDfjinnWdJnSZlx59jPTBg0K2cSBcIJAnPOs4wogRt6";

  system.stateVersion = "26.05";
  networking.hostName = "jupiter";
  networking.networkmanager.enable = true;
  time.timeZone = "America/Chicago";
  i18n.defaultLocale = "en_US.UTF-8";

  boot.loader.systemd-boot.enable = true;
  boot.loader.systemd-boot.configurationLimit = 6;
  boot.loader.efi.canTouchEfiVariables = true;
  boot.initrd.systemd.enable = true;

  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  nix.gc = {
    automatic = true;
    dates = "weekly";
    options = "--delete-older-than 30d";
  };
  services.fstrim.enable = true;
  zramSwap.enable = true;

  services.openssh = {
    enable = true;
    openFirewall = true;
    settings = {
      PermitRootLogin = "prohibit-password";
      PasswordAuthentication = false;
      KbdInteractiveAuthentication = false;
    };
  };
  networking.firewall.enable = true;

  users.users.root.openssh.authorizedKeys.keys = [
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdm4iJJvXlnvitJFj8mKzxBIzmxRXcTPTZoWV3wzFm/ mesh@venus-20260731"
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJKydheFJFdyPoYRYZxefcxVKPmw+ImnB3cklKo/V3tH root@kvm-2e1c"
  ];
  users.users.devon = {
    isNormalUser = true;
    description = "Compute operator";
    extraGroups = [ "wheel" "networkmanager" ];
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdm4iJJvXlnvitJFj8mKzxBIzmxRXcTPTZoWV3wzFm/ mesh@venus-20260731"
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJKydheFJFdyPoYRYZxefcxVKPmw+ImnB3cklKo/V3tH root@kvm-2e1c"
    ];
  };

  environment.systemPackages = with pkgs; [
    curl wget git vim nano tmux htop btop
    pciutils usbutils ethtool lm_sensors
    k3s kubectl
  ];

  # Enrollment requires the dedicated Kubernetes control plane and its node token.
  services.k3s.enable = false;
}
