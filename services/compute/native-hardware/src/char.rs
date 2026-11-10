//! Immutable native allowlist for accelerator character-device mappings.
use std::env;
use std::fs;
use std::os::unix::fs::{FileTypeExt, MetadataExt};
use std::path::{Component, Path};

pub(crate) fn map_char(kind: &str, path: &str) -> Result<(String, u32), &'static str> {
    if !matches!(kind, "gpu" | "npu" | "fpga") {
        return Err("unknown accelerator device class");
    }
    if !path.starts_with("/dev/") || path.len() > 255
        || !path.bytes().all(|b| b.is_ascii_alphanumeric()
            || matches!(b, b'/' | b'.' | b'_' | b'-' | b':'))
        || Path::new(path).components().any(|c|
            matches!(c, Component::ParentDir | Component::CurDir))
    {
        return Err("invalid accelerator device path");
    }
    let policy = env::var("HOMELAB_NATIVE_DEVICE_POLICY")
        .map_err(|_| "native accelerator policy is not configured")?;
    let file = Path::new(&policy);
    if !file.starts_with("/nix/store") || !file.is_absolute() {
        return Err("native accelerator policy must be a Nix store file");
    }
    let meta = fs::symlink_metadata(file).map_err(|_| "native device policy unavailable")?;
    if !meta.is_file() || meta.file_type().is_symlink()
        || meta.mode() & 0o222 != 0 || meta.len() > 64 * 1024
    {
        return Err("native policy has unsafe custody");
    }
    let contents = fs::read_to_string(file).map_err(|_| "cannot read native policy")?;
    let allowed = contents.lines().any(|line| {
        line.split_once(' ').is_some_and(|(selected_kind, selected_path)|
            selected_kind == kind && selected_path == path)
    });
    if !allowed {
        return Err("device is not in native operator allowlist");
    }
    let device = fs::symlink_metadata(path).map_err(|_| "accelerator device unavailable")?;
    if !device.file_type().is_char_device() {
        return Err("approved accelerator is not a character device");
    }
    Ok((path.to_owned(), device.gid()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn denies_unsupported_device_classes_and_paths() {
        assert!(map_char("other", "/dev/uio0").is_err());
        assert!(map_char("fpga", "/dev/../../etc/passwd").is_err());
        assert!(map_char("gpu", "/dev/tty0").is_err());
    }
}
