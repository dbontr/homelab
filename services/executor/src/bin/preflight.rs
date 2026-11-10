//! Validate an independent owner-only copy of the executor state before migration.
use std::env;
use std::fs;
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::Arc;

use homelab_executor_authority::audit::AuditJournal;
use homelab_executor_authority::policy::Policy;
use homelab_executor_authority::release::ReleaseController;

fn private_copy(directory: &Path, live: &Path) -> Result<(), &'static str> {
    if !directory.is_absolute() || directory == live ||
        directory.components().any(|c| matches!(c, std::path::Component::ParentDir)) {
        return Err("preflight requires an independent absolute state copy");
    }
    let copy_meta = fs::symlink_metadata(directory).map_err(|_| "executor state copy missing")?;
    if !copy_meta.is_dir() || copy_meta.file_type().is_symlink()
        || copy_meta.permissions().mode() & 0o077 != 0
        || copy_meta.uid() != unsafe { libc::geteuid() } {
        return Err("state copy is not owner-only");
    }
    if let Ok(live_meta) = fs::metadata(live) {
        if copy_meta.dev() == live_meta.dev() && copy_meta.ino() == live_meta.ino() {
            return Err("state copy aliases a live directory inode");
        }
    }
    for name in ["executor-actions.jsonl", ".executord.lock", ".lock", "journal.json"] {
        let copy_file = directory.join(name);
        if let Ok(copy) = fs::symlink_metadata(&copy_file) {
            if copy.file_type().is_symlink() || !copy.is_file() {
                return Err("copied executor state contains an unsafe entry");
            }
            if let Ok(active) = fs::metadata(live.join(name)) {
                if copy.dev() == active.dev() && copy.ino() == active.ino() {
                    return Err("copied executor record aliases a live inode");
                }
            }
        }
    }
    Ok(())
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if unsafe { libc::geteuid() } != 0 { return Err("executor preflight requires trusted root".into()); }
    let args: Vec<String> = env::args().skip(1).collect();
    if args.len() != 2 || args[0] != "--state-dir" {
        return Err("usage: preflight --state-dir OWNER_ONLY_COPY".into());
    }
    let mut policy = Policy::load()?;
    let live = policy.state_dir.clone();
    let copy = PathBuf::from(&args[1]);
    private_copy(&copy, &live)?;
    if !copy.join("executor-actions.jsonl").is_file() {
        return Err("copied privileged audit journal is required".into());
    }
    // Opening the copied journal takes the same exclusive process lease and
    // validates the complete transition history without appending an event.
    let _audit = AuditJournal::open(&copy.join("executor-actions.jsonl"))?;
    policy.state_dir = copy.clone();
    let controller = ReleaseController::new(Arc::new(policy));
    for entry in fs::read_dir(&copy)? {
        let path = entry?.path();
        let Some(name) = path.file_name().and_then(|s| s.to_str()) else {
            return Err("copied state has an invalid name".into());
        };
        if name.starts_with("stage-") && name.ends_with(".json") {
            let id = name.strip_prefix("stage-").unwrap_or("")
                .strip_suffix(".json").unwrap_or("");
            controller.verify_stage(id).await?;
        }
    }
    if copy.join("journal.json").exists() {
        controller.status().await?;
    }
    println!("copied executor audit and staged release records accepted");
    Ok(())
}
