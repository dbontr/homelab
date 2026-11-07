{ config, lib, pkgs, ... }:
{
  imports = [ ./hardware-configuration.nix ./homelab-worker.nix ../common/eda-build-vm.nix ./eda-node-settings.nix ];
  boot.loader.systemd-boot.enable = true;
  boot.loader.efi.canTouchEfiVariables = true;
  networking.hostName = "janus";
  networking.useDHCP = false;
  networking.interfaces.enp0s31f6.ipv4.addresses = [
    { address = "10.77.30.232"; prefixLength = 24; }
  ];
  networking.defaultGateway = "10.77.30.1";
  networking.nameservers = [ "10.77.30.1" "1.1.1.1" ];
  time.timeZone = "America/Chicago";
  users.users.devon = { isNormalUser = true; extraGroups = [ "wheel" ]; };
  environment.systemPackages = with pkgs; [ vim curl git htop ];
  zramSwap.enable = true;
  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  system.stateVersion = "26.05";
}
