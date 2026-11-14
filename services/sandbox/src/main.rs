//! Revision-bound, network-isolated verification of approved workspaces.
mod contained;

use std::env;
use std::fs;
use std::os::unix::fs::MetadataExt;
use std::path::{Component, Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

fn hex(value: &str, sizes: &[usize]) -> bool {
    sizes.contains(&value.len()) && value.bytes().all(|b|
        b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn trusted_binary(key: &str) -> Result<PathBuf, &'static str> {
    let raw = env::var(key).map_err(|_| "sandbox tool is not configured")?;
    let path = PathBuf::from(raw);
    if !path.is_absolute() || !path.starts_with("/nix/store") ||
        path.components().any(|c| matches!(c, Component::ParentDir | Component::CurDir)) {
        return Err("sandbox tool must be an immutable store path");
    }
    let meta = fs::metadata(&path).map_err(|_| "sandbox tool is unavailable")?;
    if !meta.is_file() || meta.mode() & 0o022 != 0 {
        return Err("sandbox tool has unsafe custody");
    }
    Ok(path)
}

fn workspace(id: &str) -> Result<PathBuf, &'static str> {
    if !hex(id, &[32]) { return Err("invalid workspace identity"); }
    let root = env::var("HOMELAB_SANDBOX_WORKSPACES")
        .map_err(|_| "sandbox workspace root unavailable")?;
    let root = fs::canonicalize(root).map_err(|_| "sandbox workspace root missing")?;
    if root == Path::new("/") { return Err("sandbox root cannot be /"); }
    let candidate = root.join(id);
    let md = fs::symlink_metadata(&candidate).map_err(|_| "workspace unavailable")?;
    if !md.is_dir() || md.file_type().is_symlink() || md.mode() & 0o002 != 0 {
        return Err("workspace has unsafe custody");
    }
    let result = fs::canonicalize(&candidate).map_err(|_| "workspace cannot be resolved")?;
    if result.parent() != Some(root.as_path()) {
        return Err("workspace escaped configured root");
    }
    Ok(result)
}

fn current_revision(root: &Path) -> Result<String, &'static str> {
    let git = trusted_binary("HOMELAB_SANDBOX_GIT")?;
    let output = Command::new(git)
        .args(["--no-optional-locks", "-C"])
        .arg(root)
        .args(["rev-parse", "--verify", "HEAD"])
        .env_clear().env("HOME", "/tmp").env("GIT_CONFIG_NOSYSTEM", "1")
        .stdin(Stdio::null()).output()
        .map_err(|_| "Git revision unavailable")?;
    if !output.status.success() || output.stdout.len() > 100 {
        return Err("Git revision check failed");
    }
    let revision = String::from_utf8(output.stdout).map_err(|_| "invalid Git output")?;
    let revision = revision.trim();
    if !hex(revision, &[40, 64]) { return Err("malformed source revision"); }
    Ok(revision.to_owned())
}

fn tool(action: &str) -> Result<(PathBuf, &'static [&'static str]), &'static str> {
    let (key, args): (&str, &'static [&'static str]) = match action {
        "cargo-test" => ("HOMELAB_SANDBOX_CARGO", &["test", "--locked", "--offline", "--all-targets"]),
        "cargo-check" => ("HOMELAB_SANDBOX_CARGO", &["check", "--locked", "--offline"]),
        "cargo-clippy" => ("HOMELAB_SANDBOX_CARGO",
            &["clippy", "--locked", "--offline", "--all-targets", "--", "-D", "warnings"]),
        "go-test" => ("HOMELAB_SANDBOX_GO", &["test", "-count=1", "./..."]),
        "go-vet" => ("HOMELAB_SANDBOX_GO", &["vet", "./..."]),
        "nix-check" => ("HOMELAB_SANDBOX_NIX", &["flake", "check", "--offline", "--no-build"]),
        _ => return Err("unregistered sandbox operation"),
    };
    Ok((trusted_binary(key)?, args))
}

fn wait_bounded(child: &mut Child, deadline: Instant) -> Result<(), &'static str> {
    loop {
        if let Some(status) = child.try_wait().map_err(|_| "sandbox result unavailable")? {
            return if status.success() { Ok(()) } else { Err("sandbox command failed") };
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            return Err("sandbox timed out");
        }
        thread::sleep(Duration::from_millis(50));
    }
}

fn execute(id: &str, revision: &str, action: &str) -> Result<(), &'static str> {
    unsafe extern "C" { fn geteuid() -> u32; }
    if unsafe { geteuid() } == 0 {
        return Err("engineering sandbox must not run with root authority");
    }
    if !hex(revision, &[40, 64]) { return Err("invalid revision"); }
    let root = workspace(id)?;
    if current_revision(&root)? != revision { return Err("workspace revision is stale"); }
    let (program, arguments) = tool(action)?;
    let bwrap = trusted_binary("HOMELAB_SANDBOX_BWRAP")?;
    let original = fs::metadata(&root).map_err(|_| "workspace unavailable")?;
    let mut command = Command::new(bwrap);
    command.args(["--unshare-all", "--die-with-parent", "--new-session",
        "--ro-bind", "/nix/store", "/nix/store"]);
    command.arg("--ro-bind").arg(&root).arg("/workspace");
    command.args(["--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
        "--dir", "/home", "--chdir", "/workspace",
        "--setenv", "HOME", "/tmp", "--setenv", "TMPDIR", "/tmp",
        "--setenv", "CARGO_TARGET_DIR", "/tmp/cargo-target",
        "--setenv", "GOCACHE", "/tmp/go-cache", "--setenv", "GOPATH", "/tmp/go",
        "--setenv", "GOPROXY", "off", "--setenv", "CARGO_NET_OFFLINE", "true",
        "--setenv", "LC_ALL", "C", "--"]);
    command.arg(program).args(arguments);
    command.env_clear().stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null());
    let current = fs::metadata(&root).map_err(|_| "workspace removed")?;
    if original.dev() != current.dev() || original.ino() != current.ino() {
        return Err("workspace was replaced");
    }
    let seconds = env::var("HOMELAB_SANDBOX_TIMEOUT_SECONDS").ok()
        .and_then(|value| value.parse::<u64>().ok())
        .filter(|value| (1..=1800).contains(value)).unwrap_or(300);
    let mut child = command.spawn().map_err(|_| "sandbox process unavailable")?;
    wait_bounded(&mut child, Instant::now() + Duration::from_secs(seconds))?;
    println!("completed");
    Ok(())
}

fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    let result = match args.as_slice() {
        [verb, id, revision, action] if verb == "run" => execute(id, revision, action),
        [verb, repository, revision, action] if verb == "run-contained" => contained::run(repository, revision, action),
        _ => Err("usage: homelab-engineering-sandbox run WORKSPACE_ID GIT_SHA ACTION"),
    };
    if let Err(message) = result {
        eprintln!("{message}");
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepts_only_exact_workspace_identity_and_revision() {
        assert!(hex(&"a".repeat(32), &[32]));
        assert!(hex(&"b".repeat(40), &[40, 64]));
        assert!(!hex("../../etc", &[32]));
        assert!(!hex(&"G".repeat(40), &[40, 64]));
    }

    #[test]
    fn denies_unregistered_commands_without_execution() {
        assert!(tool("sh").is_err());
        assert!(tool("git-push").is_err());
        assert!(execute("../escape", &"a".repeat(40), "go-test").is_err());
    }
}
