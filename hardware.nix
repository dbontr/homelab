{
  nodes = [
    { id = "saturn"; mode = "enrolled"; role = "controller"; system = "x86_64-linux"; }
    { id = "jupiter"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; }
    { id = "venus"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; }
    { id = "mercury"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; }
    { id = "janus"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; }
  ];

  devices = [
    {
      id = "kv260";
      kind = "fpga";
      mode = "ssh";
      connection = "ssh";
      eligibleForCompute = false;
      allowedOperations = [ "connect" "inspect" ];
    }
  ];
}
