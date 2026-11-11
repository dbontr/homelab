//! Durable admission records for raw device passthrough.
//! O_EXCL creation serializes cooperative workers and survives process loss.
//! A record may be removed only after the worker verifies container teardown.

use std::env;
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};

fn valid_workspace(value: &str) -> bool {
    value.len() == 32 &&
        value.bytes().all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}

fn directory() -> Result<PathBuf, &'static str> {
    let path = env::var("HOMELAB_NATIVE_CUSTODY_DIR")
        .unwrap_or_else(|_| "/run/homelab-device-leases".to_owned());
    if path != "/run/homelab-device-leases" &&
        path != "/var/lib/homelab-device-leases" {
        return Err("device custody must use an operator-owned state directory");
    }
    let dir = PathBuf::from(path);
    let metadata = fs::symlink_metadata(&dir)
        .map_err(|_| "device custody directory is not provisioned")?;
    if !metadata.file_type().is_dir() || metadata.permissions().mode() & 0o077 != 0 {
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

pub(crate) fn acquire(kind: &str, alias: &str, workspace: &str)
    -> Result<(String,u32), &'static str> {
    if !valid_workspace(workspace) {
        return Err("device custody requires an exact workspace identifier");
    }
    let (resolved, group) = match kind {
        "usb" => super::map_usb(alias)?,
        "gpu" | "npu" | "fpga" => super::char::map_char(kind, alias)?,
        _ => return Err("unsupported device custody class"),
    };
    let meta = fs::metadata(&resolved).map_err(|_| "leased device has disappeared")?;
    let root = directory()?;
    let record = root.join(key(kind,alias));
    let mut file = OpenOptions::new().write(true).create_new(true)
        .mode(0o600).open(&record)
        .map_err(|_| "device is leased or exclusive custody is unavailable")?;
    let contents = format!("v1\n{workspace}\n{kind}\n{alias}\n{resolved}\n{}\n",meta.rdev());
    // An incomplete record must still block reassignment after a crash.
    file.write_all(contents.as_bytes())
        .map_err(|_| "cannot record exclusive device custody")?;
    file.sync_all().map_err(|_| "cannot sync device custody")?;
    fs::File::open(&root).and_then(|dir| dir.sync_all())
        .map_err(|_| "cannot sync device custody directory")?;
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
    let record = directory()?.join(key(kind,alias));
    let meta = fs::symlink_metadata(&record)
        .map_err(|_| "device lease record is missing")?;
    if !meta.file_type().is_file() || meta.permissions().mode() & 0o077 != 0 ||
        meta.len() > 4096 {
        return Err("device lease record is unsafe");
    }
    let content = fs::read_to_string(record)
        .map_err(|_| "cannot read device custody record")?;
    let expected = format!("v1\n{workspace}\n{kind}\n{alias}\n{resolved}\n{}\n", current.rdev());
    if content != expected {
        return Err("device identity differs from its leased identity");
    }
    Ok((resolved, group))
}

fn read_owner(path: &Path) -> Result<String, &'static str> {
    let meta = fs::symlink_metadata(path).map_err(|_| "device custody record unavailable")?;
    if !meta.file_type().is_file() || meta.permissions().mode() & 0o077 != 0 ||
        meta.len() > 4096 {
        return Err("device custody record is unsafe");
    }
    let data = fs::read_to_string(path).map_err(|_| "cannot read custody record")?;
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
        if path.extension().and_then(|s|s.to_str()) != Some("lease") {
            return Err("unexpected file in exclusive custody directory");
        }
        if read_owner(&path)? == workspace {
            matches.push(path);
        }
        if matches.len() > 64 {
            return Err("too many device custody records");
        }
    }
    for path in &matches {
        // The Go controller must have already verified that the workspace
        // container is absent. This helper never destroys a running container.
        fs::remove_file(path).map_err(|_| "cannot release device custody")?;
    }
    fs::File::open(&root).and_then(|dir| dir.sync_all())
        .map_err(|_| "cannot sync released custody")?;
    Ok(matches.len())
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
    fn lease_rejects_unsupported_devices_before_filesystem_mutation() {
        assert!(acquire("unknown","/dev/null","0123456789abcdef0123456789abcdef").is_err());
        assert!(release_workspace("../other").is_err());
    }
}
