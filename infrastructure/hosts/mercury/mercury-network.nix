{ ... }:
{
  networking.useDHCP = false;
  networking.interfaces.enp1s0 = {
    useDHCP = false;
    ipv4.addresses = [
      { address = "10.77.30.231"; prefixLength = 24; }
    ];
  };
  networking.defaultGateway = {
    address = "10.77.30.1";
    interface = "enp1s0";
  };
  networking.nameservers = [ "10.77.30.1" ];
}
