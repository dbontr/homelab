let
  flake = builtins.getFlake (toString ./..);
  pkgs = import flake.inputs.nixpkgs { system = "x86_64-linux"; };
in
pkgs.testers.runNixOSTest {
  name = "release-runner-lifetime";
  nodes.machine = { pkgs, ... }: {
    system.stateVersion = "26.05";
    systemd.services.fixture-executor = {
      serviceConfig.ExecStart = "${pkgs.coreutils}/bin/sleep infinity";
    };
  };
  testScript = ''
    machine.start()
    machine.succeed("systemctl start fixture-executor.service")
    machine.succeed("systemd-run --no-block --collect --unit=fixture-release --property=Type=oneshot --property=IgnoreOnIsolate=yes /run/current-system/sw/bin/bash -c 'sleep 3; echo ready >/tmp/release-ready'")
    machine.succeed("systemctl isolate multi-user.target")
    machine.wait_until_succeeds("grep ready /tmp/release-ready")
  '';
}
