let
  flake = builtins.getFlake (toString ./..);
  pkgs = import flake.inputs.nixpkgs { system = "x86_64-linux"; };
in
pkgs.testers.runNixOSTest {
  name = "release-generation-switch";
  nodes.machine = { pkgs, ... }:
    let
      executor = pkgs.writeShellScript "fixture-rust-executor" ''
        printf 'rust\n' > /run/fixture-executor-version
        exec ${pkgs.coreutils}/bin/sleep infinity
      '';
      authority = pkgs.writeShellScript "fixture-rust-authority" ''
        printf 'rust\n' > /run/fixture-authority-version
        exec ${pkgs.coreutils}/bin/sleep infinity
      '';
    in {
      system.stateVersion = "26.05";
      system.switch.enable = true;
      boot.loader.grub.enable = pkgs.lib.mkForce false;
      boot.loader.systemd-boot.enable = pkgs.lib.mkForce false;
      systemd.services.fixture-executor = {
        wantedBy = [ "multi-user.target" ];
        serviceConfig.ExecStart = executor;
      };
      systemd.services.fixture-authority = {
        wantedBy = [ "multi-user.target" ];
        serviceConfig.ExecStart = authority;
      };
    };
  nodes.replacement = { pkgs, ... }:
    let
      executor = pkgs.writeShellScript "fixture-go-executor" ''
        printf 'go\n' > /run/fixture-executor-version
        exec ${pkgs.coreutils}/bin/sleep infinity
      '';
      authority = pkgs.writeShellScript "fixture-go-authority" ''
        printf 'go\n' > /run/fixture-authority-version
        exec ${pkgs.coreutils}/bin/sleep infinity
      '';
    in {
      system.stateVersion = "26.05";
      networking.hostName = "machine";
      system.switch.enable = true;
      boot.loader.grub.enable = pkgs.lib.mkForce false;
      boot.loader.systemd-boot.enable = pkgs.lib.mkForce false;
      systemd.services.fixture-executor = {
        wantedBy = [ "multi-user.target" ];
        serviceConfig.ExecStart = executor;
      };
      systemd.services.fixture-authority = {
        wantedBy = [ "multi-user.target" ];
        serviceConfig.ExecStart = authority;
      };
    };

  testScript = { nodes, ... }:
    let
      previous = nodes.machine.config.system.build.toplevel;
      replacement = nodes.replacement.config.system.build.toplevel;
      runner = pkgs.writeShellScript "fixture-release-runner" ''
        set -eu
        grep -Fx rust /run/fixture-executor-version
        grep -Fx rust /run/fixture-authority-version
        "${replacement}/bin/switch-to-configuration" switch
        for i in $(seq 1 40); do
          if systemctl is-active --quiet fixture-executor.service &&
             systemctl is-active --quiet fixture-authority.service &&
             grep -Fxq go /run/fixture-executor-version &&
             grep -Fxq go /run/fixture-authority-version; then
            break
          fi
          sleep 0.25
        done
        grep -Fx go /run/fixture-executor-version
        grep -Fx go /run/fixture-authority-version
        printf 'replacement-active\n' >/tmp/release-phase
        # Simulate failed target-health acceptance and restore previous units.
        "${previous}/bin/switch-to-configuration" switch
        for i in $(seq 1 40); do
          if systemctl is-active --quiet fixture-executor.service &&
             systemctl is-active --quiet fixture-authority.service &&
             grep -Fxq rust /run/fixture-executor-version &&
             grep -Fxq rust /run/fixture-authority-version; then
            break
          fi
          sleep 0.25
        done
        grep -Fx rust /run/fixture-executor-version
        grep -Fx rust /run/fixture-authority-version
        printf 'rolled-back\n' >/tmp/release-phase
      '';
    in
    ''
      machine.start()
      machine.wait_for_unit("fixture-executor.service")
      machine.wait_for_unit("fixture-authority.service")
      machine.succeed("grep -Fx rust /run/fixture-executor-version")
      machine.succeed("grep -Fx rust /run/fixture-authority-version")
      machine.succeed("systemd-run --no-block --collect --unit=fixture-release --property=Type=oneshot --property=TimeoutStartSec=3min --property=IgnoreOnIsolate=yes ${runner}")
      machine.wait_until_succeeds("test -f /tmp/release-phase && grep -Fx rolled-back /tmp/release-phase", timeout=100)
      machine.succeed("systemctl is-active fixture-executor.service fixture-authority.service")
      machine.succeed("grep -Fx rust /run/fixture-executor-version")
      machine.succeed("grep -Fx rust /run/fixture-authority-version")
    '';
}
