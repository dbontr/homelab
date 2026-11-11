//! Durable admission records for raw device passthrough.
//! O_EXCL creation serializes cooperative workers and survives process loss.
//! A record may be removed only after the worker verifies container teardown.

use std::env;
use std::fs::{self, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt};
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

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
    let file = fs::File::open(path).map_err(|_| "device custody record unavailable")?;
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

// The native authority independently checks the exact managed container
// before freeing any raw device. The Docker executable path is compiled into
// the Nix release; the caller cannot replace it through environment variables.
fn container_absent(workspace: &str) -> Result<(), &'static str> {
    if !valid_workspace(workspace) {
        return Err("invalid managed workspace identity");
    }
    let binary = option_env!("HOMELAB_NATIVE_DOCKER_BIN")
        .ok_or("Docker absence verification is not packaged")?;
    if !Path::new(binary).is_absolute() {
        return Err("Docker inspection executable is not absolute");
    }
    let name = format!("name=^/pwm-{workspace}$");
    let mut child = Command::new(binary)
        .args([
            "--host", "unix:///var/run/docker.sock",
            "ps", "-a", "--filter", &name, "--format", "{{.Names}}",
        ])
        .env_clear()
        .env("LC_ALL", "C")
        .env("HOME", "/var/empty")
        .env("DOCKER_HOST", "unix:///var/run/docker.sock")
        .env("DOCKER_CONTEXT", "")
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|_| "independent Docker custody inspection unavailable")?;
    let deadline = Instant::now() + Duration::from_secs(10);
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => {
                thread::sleep(Duration::from_millis(25));
            }
            Ok(None) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err("Docker custody inspection exceeded its deadline");
            }
            Err(_) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err("Docker custody inspection failed");
            }
        }
    };
    let output = child.wait_with_output()
        .map_err(|_| "Docker custody inspection output unavailable")?;
    if !status.success() || output.stdout.len() > 1024 {
        return Err("Docker custody state cannot be verified");
    }
    if !output.stdout.iter().all(u8::is_ascii_whitespace) {
        return Err("managed workspace still exists in Docker");
    }
    Ok(())
}

fn sync_directory(root: &Path) -> Result<(), &'static str> {
    fs::File::open(root).and_then(|dir| dir.sync_all())
        .map_err(|_| "cannot sync device custody directory")
}

fn record_contents(workspace: &str, kind: &str, alias: &str,
    resolved: &str, metadata: &fs::Metadata) -> String {
    format!("v2\\n{workspace}\\n{kind}\\n{alias}\\n{resolved}\\n{}\\n{}\\n{}\\n",
        metadata.rdev(), metadata.dev(), metadata.ino())
}

fn physical_from_record(kind: &str, alias: &str, body: &str)
    -> Result<String, &'static str> {
    let rows: Vec<&str> = body.lines().collect();
    if rows.len() != 8 || rows[0] != "v2" ||
        rows[2] != kind || rows[3] != alias {
        return Err("device custody record has an incompatible identity");
    }
    let number = rows[5].parse::<u64>()
        .map_err(|_| "device custody record has an invalid device number")?;
    rows[6].parse::<u64>().map_err(|_| "invalid device filesystem number")?;
    rows[7].parse::<u64>().map_err(|_| "invalid device inode number")?;
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
    let contents = record_contents(workspace,kind,alias,&resolved,&meta);
    // Reserve the physical identity first. An interrupted allocation cannot
    // abandon a reusable alias while another process owns the same device.
    reserve_record(&physical, &contents)?;
    sync_directory(&root)?;
    reserve_record(&record, &contents)?;
    sync_directory(&root)?;
    let current = fs::metadata(&resolved)
        .map_err(|_| "leased device disappeared during admission")?;
    if current.rdev() != meta.rdev() || current.dev() != meta.dev()
        || current.ino() != meta.ino() {
        return Err("device node was replaced during custody acquisition");
    }
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
    let expected = record_contents(workspace,kind,alias,&resolved,&current);
    if content != expected ||
        private_record(&root.join(physical_key(kind,alias,current.rdev())))? != expected {
        return Err("kernel device identity differs from its exclusive lease");
    }
    Ok((resolved, group))
}

fn read_owner(path: &Path) -> Result<String, &'static str> {
    let data = private_record(path)?;
    let mut lines = data.lines();
    if lines.next() != Some("v2") {
        return Err("unsupported custody record version");
    }
    let owner = lines.next().ok_or("custody record is incomplete")?;
    if !valid_workspace(owner) || lines.count() != 6 {
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
    container_absent(workspace)?;
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
            if rows.len() != 8 ||
                physical_from_record(rows[2], rows[3], &body)? !=
                    path.file_name().and_then(|name| name.to_str()).unwrap_or("") ||
                private_record(&root.join(key(rows[2], rows[3])))? != body {
                return Err("physical custody record has no matching alias");
            }
            continue;
        }
        if owner != workspace { continue; }
        let contents = private_record(&path)?;
        let rows: Vec<&str> = contents.lines().collect();
        if rows.len() != 8 {
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
    container_absent(workspace)?;
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
            "v2\n0123456789abcdef0123456789abcdef\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n22\n998\n"
        ).unwrap(), "char-0000000000000103.physical");
        assert!(physical_from_record(
            "npu", "/dev/dri/renderD128",
            "v2\n0123456789abcdef0123456789abcdef\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n22\n998\n"
        ).is_err());
    }

    #[test]
    fn physical_record_rejects_legacy_or_incomplete_kernel_identity() {
        let legacy = "v1\n0123456789abcdef0123456789abcdef\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n";
        assert!(physical_from_record("gpu", "/dev/dri/renderD128", legacy).is_err());
        let no_inode = "v2\n0123456789abcdef0123456789abcdef\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n22\n";
        assert!(physical_from_record("gpu", "/dev/dri/renderD128", no_inode).is_err());
        let invalid_inode = "v2\n0123456789abcdef0123456789abcdef\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n22\ninvalid\n";
        assert!(physical_from_record("gpu", "/dev/dri/renderD128", invalid_inode).is_err());
    }

    #[test]
    fn replaced_nodes_cannot_reuse_the_same_kernel_device_number() {
        let root = std::env::temp_dir().join(format!(
            "native-custody-identity-{}-{:?}",std::process::id(),
            std::thread::current().id()));
        fs::create_dir(&root).unwrap();
        let first = root.join("first");
        let second = root.join("second");
        fs::write(&first,b"first").unwrap();
        fs::write(&second,b"second").unwrap();
        let old = fs::metadata(&first).unwrap();
        let current = fs::metadata(&second).unwrap();
        assert_eq!(old.rdev(),current.rdev());
        assert_ne!(old.ino(),current.ino());
        let owned = record_contents(
            "0123456789abcdef0123456789abcdef","gpu",
            "/dev/dri/renderD128","/dev/dri/renderD128",&old);
        let substituted = record_contents(
            "0123456789abcdef0123456789abcdef","gpu",
            "/dev/dri/renderD128","/dev/dri/renderD128",&current);
        assert_ne!(owned,substituted);
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn native_teardown_requires_a_pinned_independent_docker_inspector() {
        // A standalone unconfigured source build has no baked-in Docker
        // executable. It must not release a live resource on that basis.
        if option_env!("HOMELAB_NATIVE_DOCKER_BIN").is_none() {
            assert!(container_absent("0123456789abcdef0123456789abcdef").is_err());
        }
        assert!(container_absent("../some-other-workspace").is_err());
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
