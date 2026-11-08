{ hardwareNode, ... }:
{
  networking.useDHCP = false;
  networking.interfaces.${hardwareNode.lanInterface} = {
    useDHCP = false;
    ipv4.addresses = [
      { address = hardwareNode.nodeIP; prefixLength = 24; }
    ];
  };
  networking.defaultGateway = {
    address = "10.77.30.1";
    interface = hardwareNode.lanInterface;
  };
  networking.nameservers = [ "10.77.30.1" ];
}
