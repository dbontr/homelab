//! Validate a leased USB device at the worker's native execution boundary.
//! The caller controls scheduling; this binary checks the live Linux identity.
mod char;
mod custody;

use std::env;
use std::fs;
use std::os::unix::fs::{FileTypeExt, MetadataExt};
use std::path::{Component, Path};
use std::process::{Command, Stdio};

fn safe_component(value: &str) -> bool {
    !value.is_empty()
        && value.bytes().all(|b| b.is_ascii_alphanumeric()
            || matches!(b, b'.' | b'_' | b':' | b'+' | b'-'))
}

fn approved_alias(path: &str) -> Option<bool> {
    if let Some(rest) = path.strip_prefix("/dev/disk/by-id/usb-") {
        return (safe_component(rest) && !rest.contains("-part")).then_some(true);
    }
    if let Some(rest) = path.strip_prefix("/dev/serial/by-id/") {
        return safe_component(rest).then_some(false);
    }
    if let Some(rest) = path.strip_prefix("/dev/bus/usb/") {
        let mut parts = rest.split('/');
        let (Some(bus), Some(slot), None) = (parts.next(), parts.next(), parts.next()) else {
            return None;
        };
        if bus.len() == 3 && slot.len() == 3
            && bus.bytes().all(|b| b.is_ascii_digit())
            && slot.bytes().all(|b| b.is_ascii_digit())
        {
            return Some(false);
        }
    }
    None
}

fn linux_device_number(raw: u64) -> (u64, u64) {
    let major = ((raw >> 8) & 0xfff) | ((raw >> 32) & !0xfffu64);
    let minor = (raw & 0xff) | ((raw >> 12) & 0xffffff00);
    (major, minor)
}

fn usb_bus_ancestor(path: &Path) -> bool {
    path.components().any(|component| match component {
        Component::Normal(segment) => segment.to_str().is_some_and(|part| {
            part.len() > 3 && part.starts_with("usb")
                && part[3..].bytes().all(|b| b.is_ascii_digit())
        }),
        _ => false,
    })
}

fn checked_lsblk(binary: &str, flags: &[&str], device: &Path) -> Result<String, &'static str> {
    let executable = Path::new(binary);
    if !executable.is_absolute() || binary.contains('\0') {
        return Err("invalid disk inspection binary");
    }
    let output = Command::new(executable)
        .args(flags)
        .arg(device)
        .env_clear()
        .env("LC_ALL", "C")
        .stdin(Stdio::null())
        .output()
        .map_err(|_| "USB disk inspection unavailable")?;
    if !output.status.success() || output.stdout.len() > 16 * 1024 {
        return Err("USB disk inspection failed");
    }
    String::from_utf8(output.stdout).map_err(|_| "invalid USB disk inspection output")
}

fn disk_unused(device: &Path) -> Result<(), &'static str> {
    let binary = option_env!("HOMELAB_NATIVE_LSBLK_BIN")
        .ok_or("trusted USB disk inspector is not packaged")?;
    if checked_lsblk(&binary, &["-dnro", "TYPE"], device)?.trim() != "disk" {
        return Err("USB mapping must identify a whole disk");
    }
    let kinds = checked_lsblk(&binary, &["-nro", "TYPE"], device)?;
    if kinds.lines().any(|line| !matches!(line.trim(), "disk" | "part")) {
        return Err("USB device backs an active mapped device");
    }
    let mounts = checked_lsblk(&binary, &["-nro", "MOUNTPOINTS"], device)?;
    if !mounts.trim().is_empty() {
        return Err("USB disk or partition is mounted");
    }
    Ok(())
}

fn map_usb(alias: &str) -> Result<(String, u32), &'static str> {
    let block = approved_alias(alias).ok_or("USB alias is outside approved device classes")?;
    let resolved = fs::canonicalize(alias).map_err(|_| "USB alias unavailable")?;
    if !resolved.starts_with("/dev/") {
        return Err("USB alias escaped the device directory");
    }
    let path = resolved.to_str().ok_or("invalid USB device path")?;
    if !path.bytes().all(|b| b.is_ascii_alphanumeric()
        || matches!(b, b'/' | b'_' | b'.' | b'-' | b':'))
    {
        return Err("USB device path has unsupported characters");
    }
    let metadata = fs::metadata(&resolved).map_err(|_| "USB device missing")?;
    if block && !metadata.file_type().is_block_device()
        || !block && !metadata.file_type().is_char_device()
    {
        return Err("USB device kind differs from approved alias");
    }
    let (major, minor) = linux_device_number(metadata.rdev());
    let device_class = if block { "block" } else { "char" };
    let sysfs = fs::canonicalize(format!("/sys/dev/{device_class}/{major}:{minor}"))
        .map_err(|_| "USB sysfs identity unavailable")?;
    if !usb_bus_ancestor(&sysfs) {
        return Err("device is not attached to a USB bus");
    }
    if block {
        disk_unused(&resolved)?;
    }
    Ok((path.to_owned(), metadata.gid()))
}

fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    if let [version, verb, kind, alias, workspace] = args.as_slice() {
        if version == "custody-v1" && verb == "release-device" {
            match custody::release_device(kind, alias, workspace) {
                Ok(()) => println!("custody-v1\nreleased"),
                Err(message) => {
                    eprintln!("{message}");
                    std::process::exit(1);
                }
            }
            return;
        }
    }
    if let [version, verb, workspace] = args.as_slice() {
        if version == "custody-v1" && verb == "verify-workspace" {
            match custody::verify_workspace(workspace) {
                Ok(count) => println!("custody-v1\n{count}"),
                Err(message) => {
                    eprintln!("{message}");
                    std::process::exit(1);
                }
            }
            return;
        }
        if version == "custody-v1" && verb == "release-workspace" {
            match custody::release_workspace(workspace) {
                Ok(count) => println!("custody-v1\n{count}"),
                Err(message) => {
                    eprintln!("{message}");
                    std::process::exit(1);
                }
            }
            return;
        }
    }
    let (custody_protocol, result) = match args.as_slice() {
        [verb, alias] if verb == "map-usb" => (false, map_usb(alias)),
        [verb, kind, path] if verb == "map-char" => (false, char::map_char(kind,path)),
        [version, verb, kind, alias, workspace]
            if version == "custody-v1" && verb == "acquire-device" =>
                (true, custody::acquire(kind,alias,workspace)),
        [version, verb, kind, alias, workspace]
            if version == "custody-v1" && verb == "verify-device" =>
                (true, custody::verify(kind,alias,workspace)),
        _ => (false, Err("usage: homelab-native-hardware <map-usb ALIAS | map-char KIND PATH | custody-v1 acquire-device KIND PATH WORKSPACE | custody-v1 verify-device KIND PATH WORKSPACE | custody-v1 release-device KIND PATH WORKSPACE | custody-v1 verify-workspace WORKSPACE | custody-v1 release-workspace WORKSPACE>")),
    };
    match result {
        Ok((resolved,group)) if custody_protocol =>
            println!("custody-v1\n{resolved}\n{group}"),
        Ok((resolved,group)) => println!("{resolved}\n{group}"),
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(1);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_stable_usb_aliases_are_eligible() {
        assert_eq!(approved_alias("/dev/disk/by-id/usb-Foo_123"), Some(true));
        assert_eq!(approved_alias("/dev/serial/by-id/usb-ACME_99"), Some(false));
        assert_eq!(approved_alias("/dev/bus/usb/002/013"), Some(false));
        for path in [
            "/dev/sda", "/dev/disk/by-id/ata-system", "/dev/disk/by-id/usb-SSD-part1",
            "/dev/serial/by-id/usb-a/../../etc/passwd", "/dev/bus/usb/2/3",
            "/dev/bus/usb/001/002/003", "/dev/disk/by-id/usb-a\nb",
        ] {
            assert!(approved_alias(path).is_none(), "{path:?}");
        }
    }

    #[test]
    fn identifies_a_real_usb_bus_ancestor() {
        assert!(usb_bus_ancestor(Path::new("/sys/devices/x/usb3/3-1/tty")));
        assert!(!usb_bus_ancestor(Path::new("/sys/devices/pci0000:00/nvme/nvme0")));
        assert!(!usb_bus_ancestor(Path::new("/sys/devices/x/usbX/tty")));
    }

    #[test]
    fn mapping_nonexistent_usb_is_denied() {
        assert!(map_usb("/dev/disk/by-id/usb-no-such-device").is_err());
    }
}
