//! Durable admission records for raw device passthrough.
//! O_EXCL creation serializes cooperative workers and survives process loss.
//! A record may be removed only after the worker verifies container teardown.

use std::env;
use std::fs::{self, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt};

extern "C" { fn geteuid() -> u32; }
fn owner() -> u32 { unsafe { geteuid() } }

use std::path::{Path, PathBuf};

fn valid_workspace(value: &str) -> bool {
    value.len() == 32 &&
        value.bytes().all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}

fn directory() -> Result<PathBuf, &'static str> {
    let path = env::var("HOMELAB_NATIVE_CUSTODY_DIR")
        .unwrap_or_else(|_| "/var/lib/homelab-device-leases".to_owned());
    if path != "/run/homelab-device-leases" &&
        path != "/var/lib/homelab-device-leases" {
        return Err("device custody must use an operator-owned state directory");
    }
    let dir = PathBuf::from(path);
    let metadata = fs::symlink_metadata(&dir)
        .map_err(|_| "device custody directory is not provisioned")?;
    if !metadata.file_type().is_dir() || metadata.permissions().mode() & 0o077 != 0 ||
        metadata.uid() != owner() ||
        fs::canonicalize(&dir).map_err(|_| "cannot verify custody directory")? != dir {
        return Err("device custody directory must be private and cannot be a symlink");
    }
    Ok(dir)
}

// FNV-1a makes a fixed-size filesystem key, not an authentication token.
// The complete device identity is retained in the protected record.
fn key(kind: &str, alias: &str) -> String {
    let mut hash = 0xcbf29ce484222325_u64;
    for byte in kind.bytes().chain(std::iter::once(0)).chain(alias.bytes()) {
        hash ^= u64::from(byte);
        hash = hash.wrapping_mul(0x100000001b3);
    }
    format!("{hash:016x}.lease")
}

// A single kernel device number can have multiple valid aliases. A second
// durable key prevents two otherwise distinct names from leasing one device.
fn physical_key(kind: &str, alias: &str, number: u64) -> String {
    let class = if kind == "usb" && alias.starts_with("/dev/disk/by-id/usb-") {
        "block"
    } else {
        "char"
    };
    format!("{class}-{number:016x}.physical")
}

fn private_record(path: &Path) -> Result<String, &'static str> {
    let mut file = fs::File::open(path).map_err(|_| "device custody record unavailable")?;
    let opened = file.metadata().map_err(|_| "device custody metadata unavailable")?;
    let named = fs::symlink_metadata(path).map_err(|_| "device custody path unavailable")?;
    if !named.file_type().is_file() || !opened.is_file() ||
        named.nlink() != 1 || named.uid() != owner() ||
        named.mode() & 0o077 != 0 || named.dev() != opened.dev() ||
        named.ino() != opened.ino() || opened.len() == 0 || opened.len() > 4096 {
        return Err("device custody record has unsafe ownership or identity");
    }
    let mut body = String::new();
    file.take(4097).read_to_string(&mut body)
        .map_err(|_| "device custody record cannot be read")?;
    if body.is_empty() || body.len() > 4096 {
        return Err("device custody record exceeds its limit");
    }
    Ok(body)
}

fn reserve_record(path: &Path, contents: &str) -> Result<(), &'static str> {
    let mut file = OpenOptions::new().write(true).create_new(true).mode(0o600)
        .open(path).map_err(|_| "device is leased or custody is unavailable")?;
    file.write_all(contents.as_bytes()).and_then(|()| file.sync_all())
        .map_err(|_| "cannot persist exclusive device custody")
}

fn sync_directory(root: &Path) -> Result<(), &'static str> {
    fs::File::open(root).and_then(|dir| dir.sync_all())
        .map_err(|_| "cannot sync device custody directory")
}

fn physical_from_record(kind: &str, alias: &str, body: &str)
    -> Result<String, &'static str> {
    let rows: Vec<&str> = body.lines().collect();
    if rows.len() != 6 || rows[0] != "v1" ||
        rows[2] != kind || rows[3] != alias {
        return Err("device custody record has an incompatible identity");
    }
    let number = rows[5].parse::<u64>()
        .map_err(|_| "device custody record has an invalid device number")?;
    Ok(physical_key(kind, alias, number))
}

pub(crate) fn acquire(kind: &str, alias: &str, workspace: &str)
    -> Result<(String,u32), &'static str> {
    if !valid_workspace(workspace) {
        return Err("device custody requires an exact workspace identifier");
    }
    if kind == "usb" && alias.starts_with("/dev/bus/usb/") {
        // Kernel USB bus addresses can be reassigned to another physical
        // device after unplug. They remain eligible for inspection, never
        // for durable raw-device allocation.
        return Err("durable USB custody requires a stable by-id alias");
    }
    let (resolved, group) = match kind {
        "usb" => super::map_usb(alias)?,
        "gpu" | "npu" | "fpga" => super::char::map_char(kind, alias)?,
        _ => return Err("unsupported device custody class"),
    };
    let meta = fs::metadata(&resolved).map_err(|_| "leased device has disappeared")?;
    let root = directory()?;
    let record = root.join(key(kind,alias));
    let physical = root.join(physical_key(kind, alias, meta.rdev()));
    let contents = format!("v1\n{workspace}\n{kind}\n{alias}\n{resolved}\n{}\n",meta.rdev());
    // Reserve the physical identity first. An interrupted allocation cannot
    // abandon a reusable alias while another process owns the same device.
    reserve_record(&physical, &contents)?;
    sync_directory(&root)?;
    reserve_record(&record, &contents)?;
    sync_directory(&root)?;
    Ok((resolved, group))
}

// Revalidate the exact kernel device identity immediately before and after
// container allocation. A changed path or a stolen lease is denied.
pub(crate) fn verify(kind: &str, alias: &str, workspace: &str)
    -> Result<(String,u32), &'static str> {
    if !valid_workspace(workspace) {
        return Err("device custody requires an exact workspace identifier");
    }
    let (resolved, group) = match kind {
        "usb" => super::map_usb(alias)?,
        "gpu" | "npu" | "fpga" => super::char::map_char(kind, alias)?,
        _ => return Err("unsupported device custody class"),
    };
    let current = fs::metadata(&resolved)
        .map_err(|_| "leased device is missing")?;
    let root = directory()?;
    let record = root.join(key(kind,alias));
    let content = private_record(&record)?;
    let expected = format!("v1\n{workspace}\n{kind}\n{alias}\n{resolved}\n{}\n", current.rdev());
    if content != expected ||
        private_record(&root.join(physical_key(kind,alias,current.rdev())))? != expected {
        return Err("kernel device identity differs from its exclusive lease");
    }
    Ok((resolved, group))
}

fn read_owner(path: &Path) -> Result<String, &'static str> {
    let data = private_record(path)?;
    let mut lines = data.lines();
    if lines.next() != Some("v1") {
        return Err("unsupported custody record version");
    }
    let owner = lines.next().ok_or("custody record is incomplete")?;
    if !valid_workspace(owner) || lines.count() != 4 {
        return Err("custody record has invalid identity or contents");
    }
    Ok(owner.to_owned())
}

// Rollback removes exactly one newly acquired record. A previous workspace's
// record is never released merely because a later multi-device claim failed.
pub(crate) fn release_device(kind: &str, alias: &str, workspace: &str)
    -> Result<(), &'static str> {
    if !valid_workspace(workspace) || !matches!(kind, "usb" | "gpu" | "npu" | "fpga") {
        return Err("invalid device custody release identity");
    }
    let root = directory()?;
    let record = root.join(key(kind,alias));
    if read_owner(&record)? != workspace {
        return Err("device custody belongs to another workspace");
    }
    let contents = private_record(&record)?;
    let physical = root.join(physical_from_record(kind, alias, &contents)?);
    if private_record(&physical)? != contents {
        return Err("physical custody differs from its alias record");
    }
    // Remove the alias before its physical guard. A crash leaves the device
    // blocked until independently verified teardown and reconciliation.
    fs::remove_file(&record).map_err(|_| "cannot release alias custody")?;
    sync_directory(&root)?;
    fs::remove_file(&physical).map_err(|_| "cannot release physical custody")?;
    sync_directory(&root)?;
    Ok(())
}

// Every request that touches an existing workspace rechecks its active raw
// devices. Device removal, alias replacement, or a malformed lease denies
// further work until the controller has stopped and reconciled the container.
pub(crate) fn verify_workspace(workspace: &str) -> Result<usize, &'static str> {
    if !valid_workspace(workspace) {
        return Err("device custody requires an exact workspace identifier");
    }
    let root = directory()?;
    let mut count = 0usize;
    let entries = fs::read_dir(root)
        .map_err(|_| "device custody inventory unavailable")?;
    for entry in entries {
        let entry = entry.map_err(|_| "device custody inventory unavailable")?;
        let path = entry.path();
        let suffix = path.extension().and_then(|ext| ext.to_str());
        if !matches!(suffix, Some("lease" | "physical")) {
            return Err("unexpected device custody entry");
        }
        let owner = read_owner(&path)?;
        if suffix == Some("physical") {
            let body = private_record(&path)?;
            let rows: Vec<&str> = body.lines().collect();
            if rows.len() != 6 ||
                physical_from_record(rows[2], rows[3], &body)? !=
                    path.file_name().and_then(|name| name.to_str()).unwrap_or("") ||
                private_record(&root.join(key(rows[2], rows[3])))? != body {
                return Err("physical custody record has no matching alias");
            }
            continue;
        }
        if owner != workspace { continue; }
        let contents = fs::read_to_string(&path)
            .map_err(|_| "cannot read device custody record")?;
        let rows: Vec<&str> = contents.lines().collect();
        if rows.len() != 6 {
            return Err("device custody record is incomplete");
        }
        let kind = rows[2];
        let alias = rows[3];
        if key(kind,alias) != path.file_name().and_then(|name| name.to_str())
            .unwrap_or("") {
            return Err("device custody key does not match its contents");
        }
        verify(kind,alias,workspace)?;
        count += 1;
        if count > 64 { return Err("too many device custody records"); }
    }
    Ok(count)
}

pub(crate) fn release_workspace(workspace: &str) -> Result<usize, &'static str> {
    if !valid_workspace(workspace) {
        return Err("device custody requires an exact workspace identifier");
    }
    let root = directory()?;
    let entries = fs::read_dir(&root).map_err(|_| "cannot inspect device custody")?;
    let mut matches = Vec::new();
    for entry in entries {
        let entry = entry.map_err(|_| "device custody inventory is unavailable")?;
        let path = entry.path();
        if !matches!(path.extension().and_then(|s| s.to_str()),
            Some("lease" | "physical")) {
            return Err("unexpected file in exclusive custody directory");
        }
        let current_owner = read_owner(&path)?;
        if current_owner == workspace {
            // Workspace teardown has been proven externally. This also
            // removes a physical reservation orphaned by a partial write.
            matches.push(path);
        }
        if matches.len() > 128 {
            return Err("too many device custody records");
        }
    }
    // Release alias names first. If a process stops between unlink calls,
    // the physical reservation remains in place and blocks reassignment.
    matches.sort_by_key(|path| {
        if path.extension().and_then(|s| s.to_str()) == Some("lease") { 0 } else { 1 }
    });
    let released = matches.iter().filter(|path| {
        path.extension().and_then(|s| s.to_str()) == Some("lease")
    }).count();
    for path in &matches {
        // The Go controller must have already verified that the workspace
        // container is absent. This helper never destroys a running container.
        fs::remove_file(path).map_err(|_| "cannot release device custody")?;
    }
    fs::File::open(&root).and_then(|dir| dir.sync_all())
        .map_err(|_| "cannot sync released custody")?;
    Ok(released)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn device_keys_and_workspace_ids_are_bounded() {
        assert!(valid_workspace("0123456789abcdef0123456789abcdef"));
        assert!(!valid_workspace("0123456789ABCDEF0123456789ABCDEF"));
        assert!(!valid_workspace("../owned"));
        assert_ne!(key("gpu","/dev/dri/renderD128"),key("npu","/dev/dri/renderD128"));
        assert_eq!(key("usb","/dev/bus/usb/001/002").len(),22);
    }

    #[test]
    fn different_device_aliases_share_the_same_physical_reservation() {
        assert_eq!(
            physical_key("usb", "/dev/serial/by-id/usb-a", 0x0103),
            physical_key("gpu", "/dev/dri/renderD128", 0x0103)
        );
        assert_ne!(
            physical_key("usb", "/dev/disk/by-id/usb-a", 0x0103),
            physical_key("gpu", "/dev/dri/renderD128", 0x0103)
        );
        assert_ne!(
            physical_key("gpu", "/dev/dri/renderD128", 0x0103),
            physical_key("gpu", "/dev/dri/renderD129", 0x0104)
        );
    }

    #[test]
    fn physical_record_names_and_owners_are_strict() {
        assert_eq!(physical_from_record(
            "gpu", "/dev/dri/renderD128",
            "v1\n0123456789abcdef0123456789abcdef\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n"
        ).unwrap(), "char-0000000000000103.physical");
        assert!(physical_from_record(
            "npu", "/dev/dri/renderD128",
            "v1\n0123456789abcdef0123456789abcdef\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n"
        ).is_err());
    }

    #[test]
    fn bus_addresses_do_not_establish_durable_usb_device_identity() {
        assert!(acquire(
            "usb", "/dev/bus/usb/002/013",
            "0123456789abcdef0123456789abcdef"
        ).is_err());
    }

    #[test]
    fn lease_rejects_unsupported_devices_before_filesystem_mutation() {
        assert!(acquire("unknown","/dev/null","0123456789abcdef0123456789abcdef").is_err());
        assert!(release_workspace("../other").is_err());
    }
}
