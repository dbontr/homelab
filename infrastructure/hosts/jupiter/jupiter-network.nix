{ hardwareNode, ... }: {
  networking.networkmanager.ensureProfiles.profiles.jupiter-compute = {
    connection = {
      id = "jupiter-compute";
      type = "ethernet";
      "interface-name" = hardwareNode.lanInterface;
      autoconnect = true;
      "autoconnect-priority" = 100;
    };
    ipv4 = {
      method = "manual";
      address1 = "${hardwareNode.nodeIP}/24,10.77.30.1";
      dns = "10.77.30.1;";
    };
    ipv6.method = "disabled";
  };
}
