//! Read-only source verification inside a restricted Kubernetes Job.
use std::fs;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

fn valid_name(name: &str) -> bool {
    !name.is_empty() && name.len() <= 100 && name != "." && name != ".."
        && !name.starts_with('.') && !name.ends_with('.')
        && name.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'_' | b'-'))
}

fn checkout(repository: &str) -> Result<PathBuf, &'static str> {
    let mut pieces = repository.split('/');
    let (Some(owner), Some(name), None) = (pieces.next(), pieces.next(), pieces.next())
    else {
        return Err("repository must be owner/name");
    };
    if !valid_name(owner) || !valid_name(name) {
        return Err("invalid repository identity");
    }
    let source = Path::new("/workspace").join("repos").join(format!("{owner}__{name}"));
    let resolved = fs::canonicalize(&source)
        .map_err(|_| "repository checkout is unavailable")?;
    if resolved != source {
        return Err("repository checkout is redirected");
    }
    for ancestor in [Path::new("/workspace").join("repos")] {
        let metadata = fs::symlink_metadata(ancestor)
            .map_err(|_| "repository ancestor is unavailable")?;
        if !metadata.is_dir() || metadata.file_type().is_symlink() {
            return Err("repository ancestry is unsafe");
        }
    }
    let metadata = fs::symlink_metadata(&source)
        .map_err(|_| "checkout cannot be opened")?;
    if !metadata.is_dir() || metadata.file_type().is_symlink()
        || metadata.mode() & 0o002 != 0 {
        return Err("checkout permissions are unsafe");
    }
    Ok(source)
}

fn valid_revision(value: &str) -> bool {
    matches!(value.len(), 40 | 64)
        && value.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn source_revision(source: &Path, repository: &str, expected: &str) -> Result<(), &'static str> {
    // GitHub tarballs have no .git directory; the worker records the exact
    // controller-approved reference in a reserved import receipt.
    let marker = source.join(".pwm-source-revision");
    let meta = fs::symlink_metadata(&marker).map_err(|_| "source import receipt unavailable")?;
    if !meta.is_file() || meta.file_type().is_symlink() || meta.len() > 256 {
        return Err("invalid source import receipt");
    }
    let receipt = fs::read_to_string(&marker).map_err(|_| "source receipt unreadable")?;
    if receipt.len() > 256 || receipt != format!("{repository}\n{expected}\n") {
        return Err("source does not match the approved archive reference");
    }
    Ok(())
}

fn approved_tool(action: &str) -> Result<(&'static str, &'static [&'static str]), &'static str> {
    match action {
        "go-test" => Ok(("/usr/local/go/bin/go", &["test", "-count=1", "./..."])),
        "go-vet" => Ok(("/usr/local/go/bin/go", &["vet", "./..."])),
        "cargo-test" => Ok(("/usr/local/cargo/bin/cargo",
            &["test", "--locked", "--offline", "--all-targets"])),
        "cargo-check" => Ok(("/usr/local/cargo/bin/cargo",
            &["check", "--locked", "--offline"])),
        "cargo-clippy" => Ok(("/usr/local/cargo/bin/cargo",
            &["clippy", "--locked", "--offline", "--all-targets", "--", "-D", "warnings"])),
        _ => Err("unknown verification action"),
    }
}

fn wait(mut child: Child) -> Result<(), &'static str> {
    let deadline = Instant::now() + Duration::from_secs(600);
    loop {
        if let Some(status) = child.try_wait().map_err(|_| "verification result unavailable")? {
            return if status.success() { Ok(()) } else { Err("verification failed") };
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            return Err("verification timed out");
        }
        thread::sleep(Duration::from_millis(50));
    }
}

pub(crate) fn run(repository: &str, revision: &str, action: &str) -> Result<(), &'static str> {
    unsafe extern "C" { fn geteuid() -> u32; }
    if unsafe { geteuid() } == 0 { return Err("verification must not run as root"); }
    if !valid_revision(revision) { return Err("invalid expected Git revision"); }
    let source = checkout(repository)?;
    source_revision(&source, repository, revision)?;
    let (binary, args) = approved_tool(action)?;
    let before = fs::metadata(&source).map_err(|_| "checkout unavailable")?;
    let mut child = Command::new(binary);
    child.args(args).current_dir(&source).env_clear()
        .env("HOME", "/tmp").env("TMPDIR", "/tmp").env("GOENV", "off")
        .env("GOPATH", "/tmp/go").env("GOCACHE", "/tmp/go-cache")
        .env("GOPROXY", "off").env("GOSUMDB", "off").env("GOTOOLCHAIN", "local")
        .env("CARGO_HOME", "/usr/local/cargo")
        .env("CARGO_TARGET_DIR", "/tmp/cargo-target")
        .env("CARGO_NET_OFFLINE", "true").env("LC_ALL", "C")
        .env("RUSTUP_HOME", "/usr/local/rustup")
        .env("PATH", "/usr/local/go/bin:/usr/local/cargo/bin:/usr/bin:/bin")
        .stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null());
    let after = fs::metadata(&source).map_err(|_| "checkout disappeared")?;
    if before.dev() != after.dev() || before.ino() != after.ino() {
        return Err("checkout was replaced before verification");
    }
    wait(child.spawn().map_err(|_| "verification command could not start")?)?;
    source_revision(&source, repository, revision)?;
    println!("completed");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_invalid_repository_and_revision() {
        for repo in ["", ".", "owner/../repo", "owner/repo/extra", "/absolute", ".git/data"] {
            assert!(checkout(repo).is_err(), "{repo}");
        }
        assert!(valid_revision(&"a".repeat(40)));
        assert!(!valid_revision(&"A".repeat(40)));
    }

    #[test]
    fn only_build_and_test_actions_are_allowed() {
        assert!(approved_tool("go-test").is_ok());
        assert!(approved_tool("cargo-clippy").is_ok());
        for name in ["sh", "git-push", "nix-activate", "kubectl"] {
            assert!(approved_tool(name).is_err());
        }
    }
}
