{
  nodes = [
    { id = "saturn"; mode = "enrolled"; role = "controller"; system = "x86_64-linux"; }
    { id = "jupiter"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; nodeIP = "10.77.30.229"; lanInterface = "enp129s0"; }
    { id = "venus"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; nodeIP = "10.77.30.230"; lanInterface = "enp2s0"; }
    { id = "mercury"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; nodeIP = "10.77.30.231"; lanInterface = "enp1s0"; }
    { id = "janus"; mode = "enrolled"; role = "compute"; system = "x86_64-linux"; nodeIP = "10.77.30.232"; lanInterface = "enp0s31f6"; }
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
