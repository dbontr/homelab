{ config, pkgs, lib, hardwareNode, ... }:
{
  imports = [ ./hardware-configuration.nix ./mercury-network.nix ./homelab-access.nix ./homelab-worker.nix ../common/eda-build-vm.nix ./eda-node-settings.nix ];

  system.stateVersion = "26.05";
  networking.hostName = hardwareNode.id;
  networking.networkmanager.enable = false;
  time.timeZone = "America/Chicago";
  i18n.defaultLocale = "en_US.UTF-8";

  boot.loader.systemd-boot.enable = true;
  boot.loader.systemd-boot.configurationLimit = 6;
  boot.loader.efi.canTouchEfiVariables = true;
  boot.initrd.systemd.enable = true;
  # Keep the EFI boot random seed inaccessible to unprivileged users.
  fileSystems."/boot".options = lib.mkForce [ "fmask=0077" "dmask=0077" ];

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
    openFirewall = false;
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
}
