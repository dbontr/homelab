{ config, lib, pkgs, hardwareNode, ... }:
{
  imports = [ ./hardware-configuration.nix ./local-admin.nix ./homelab-worker.nix ../common/eda-build-vm.nix ./eda-node-settings.nix ];
  boot.loader.systemd-boot.enable = true;
  boot.loader.efi.canTouchEfiVariables = true;
  networking.hostName = hardwareNode.id;
  networking.useDHCP = false;
  networking.interfaces.${hardwareNode.lanInterface}.useDHCP = false;
  networking.interfaces.${hardwareNode.lanInterface}.ipv4.addresses = [
    { address = hardwareNode.nodeIP; prefixLength = 24; }
  ];
  networking.defaultGateway = {
    address = "10.77.30.1";
    interface = hardwareNode.lanInterface;
  };
  networking.nameservers = [ "10.77.30.1" ];
  networking.firewall.enable = true;
  networking.firewall.interfaces.${hardwareNode.lanInterface}.allowedTCPPorts = [ 22 9100 ];
  services.openssh.enable = true;
  services.openssh.openFirewall = false;
  services.openssh.settings = {
    PasswordAuthentication = false;
    KbdInteractiveAuthentication = false;
    PermitRootLogin = "prohibit-password";
  };
  services.prometheus.exporters.node.enable = true;
  services.prometheus.exporters.node.openFirewall = false;
  services.fstrim.enable = true;
  time.timeZone = "America/Chicago";
  swapDevices = [ { device = "/swapfile"; size = 8192; } ];
  environment.systemPackages = with pkgs; [ vim git curl jq tmux ];
  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  system.stateVersion = "26.05";
}
