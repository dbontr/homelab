package worker

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestNativeUSBMappingUsesConfiguredHelperWithoutGoFallback(t *testing.T) {
    dir := t.TempDir()
    helper := filepath.Join(dir, "native-usb")
    write := func(output string, success bool) {
        t.Helper()
        body := "#!/bin/sh\n"
        if success {
            body += "printf '" + output + "'\n"
        } else {
            body += "exit 1\n"
        }
        if err := os.WriteFile(helper, []byte(body), 0700); err != nil {
            t.Fatal(err)
        }
    }
    t.Setenv("PWM_NATIVE_HARDWARE_HELPER", helper)
    write("/dev/ttyUSB0\\n27\\n", true)
    device, group, err := usbDockerMapping("/dev/serial/by-id/usb-operator-approved")
    if err != nil || device != "/dev/ttyUSB0" || group != 27 {
        t.Fatalf("unexpected native mapping: %q %d %v", device, group, err)
    }
    for _, payload := range []string{
        "not-a-device\\n27\\n",
        "/dev/ttyUSB0\\nnot-a-number\\n",
        "/dev/ttyUSB0\\n27\\nextra\\n",
        "/dev/ttyUSB0\\n9999999999999999999\\n",
        strings.Repeat("A", 1024),
    } {
        write(payload, true)
        if _, _, err := usbDockerMapping("/dev/serial/by-id/usb-operator-approved"); err == nil {
            t.Fatalf("accepted malformed native response: %q", payload)
        }
    }
    write("", false)
    if _, _, err := usbDockerMapping("/dev/serial/by-id/usb-operator-approved"); err == nil {
        t.Fatal("native validation failure fell back to Go")
    }
}

func TestConfiguredNativeUSBHelperRequiresAbsoluteExecutable(t *testing.T) {
    t.Setenv("PWM_NATIVE_HARDWARE_HELPER", "./unsafe-helper")
    if _, _, err := usbDockerMapping("/dev/bus/usb/001/001"); err == nil {
        t.Fatal("relative native executable accepted")
    }
}

func TestNativeCharacterDeviceRequiresOperatorPolicy(t *testing.T) {
    t.Setenv("PWM_NATIVE_HARDWARE_HELPER", "/bin/false")
    t.Setenv("PWM_NATIVE_DEVICE_POLICY", "")
    if err := nativeCharMapping("fpga", "/dev/uio0"); err == nil {
        t.Fatal("native character device admitted without immutable policy")
    }
}
