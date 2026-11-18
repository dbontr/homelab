let
  flake = builtins.getFlake (toString ./..);
  pkgs = import flake.inputs.nixpkgs { system = "x86_64-linux"; };
in
pkgs.testers.runNixOSTest {
  name = "release-generation-switch";
  nodes.machine = { pkgs, ... }: {
    system.stateVersion = "26.05";
    systemd.services.fixture-executor = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig.ExecStart = "${pkgs.coreutils}/bin/sleep infinity";
    };
    systemd.services.fixture-authority = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig.ExecStart = "${pkgs.coreutils}/bin/sleep infinity";
    };
  };

  testScript = { nodes, ... }:
    let
      previous = nodes.machine.config.system.build.toplevel;
      replacement = (nodes.machine.extendModules {
        modules = [
          ({ lib, pkgs, ... }: {
            systemd.services.fixture-executor = lib.mkForce {};
            systemd.services.fixture-authority = lib.mkForce {};
            systemd.services.fixture-go-executor = {
              wantedBy = [ "multi-user.target" ];
              serviceConfig = {
                Type = "oneshot";
                RemainAfterExit = true;
                ExecStart = "${pkgs.coreutils}/bin/true";
              };
            };
            systemd.services.fixture-go-authority = {
              wantedBy = [ "multi-user.target" ];
              serviceConfig = {
                Type = "oneshot";
                RemainAfterExit = true;
                ExecStart = "${pkgs.coreutils}/bin/true";
              };
            };
          })
        ];
      }).config.system.build.toplevel;
      runner = pkgs.writeShellScript "fixture-release-runner" ''
        set -eu
        systemctl is-active --quiet fixture-executor.service
        "${replacement}/bin/switch-to-configuration" switch
        systemctl is-active --quiet fixture-go-authority.service
        systemctl is-active --quiet fixture-go-executor.service
        ! systemctl is-active --quiet fixture-executor.service
        printf 'replacement-active\n' >/tmp/release-phase
        # A deliberately failed target-health decision must restore the old generation.
        "${previous}/bin/switch-to-configuration" switch
        systemctl is-active --quiet fixture-executor.service
        systemctl is-active --quiet fixture-authority.service
        ! systemctl is-active --quiet fixture-go-authority.service
        printf 'rolled-back\n' >/tmp/release-phase
      '';
    in
    ''
      machine.start()
      machine.wait_for_unit("fixture-executor.service")
      machine.wait_for_unit("fixture-authority.service")
      machine.succeed("systemd-run --no-block --collect --unit=fixture-release --property=Type=oneshot --property=TimeoutStartSec=3min --property=IgnoreOnIsolate=yes ${runner}")
      machine.wait_until_succeeds("grep -Fx rolled-back /tmp/release-phase", timeout=180)
      machine.succeed("systemctl is-active fixture-executor.service")
      machine.succeed("systemctl is-active fixture-authority.service")
      machine.succeed("systemctl is-success fixture-release.service")
    '';
}
