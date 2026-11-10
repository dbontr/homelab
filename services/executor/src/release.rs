//! Locked, source-pinned NixOS generation staging and rollback.
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt};
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Utc};
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use tokio::io::AsyncReadExt;
use tokio::process::Command;
use tokio::sync::Mutex;

use crate::policy::Policy;
use crate::{random_hex, valid_hex, Error};

const MAX_RECORD: usize = 8192;
const PROFILE: &str = "/nix/var/nix/profiles/system";
const SWITCH: &str = "bin/switch-to-configuration";

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Manifest {
    pub id: String,
    pub host: String,
    pub revision: String,
    pub system: String,
    pub created: DateTime<Utc>,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Journal {
    pub phase: String,
    pub id: String,
    pub previous: String,
    pub target: String,
    pub time: DateTime<Utc>,
}
impl Journal {
    pub fn interrupted(&self) -> bool {
        matches!(self.phase.as_str(), "activating" | "reverting" | "rollback-failed")
    }
}

fn store_path(path: &str) -> bool {
    path.len() < 512 && path.starts_with("/nix/store/")
        && !path.bytes().any(|b| b == 0 || b.is_ascii_whitespace())
        && !Path::new(path).components().any(|c|
            matches!(c, std::path::Component::ParentDir | std::path::Component::CurDir))
}

fn require_root_owned_path(path: &Path) -> Result<(), Error> {
    if !path.is_absolute() { return Err(Error::Configuration("trusted release path must be absolute")); }
    let mut current = path;
    loop {
        let md = fs::symlink_metadata(current)
            .map_err(|_| Error::Storage("trusted release path unavailable"))?;
        if md.file_type().is_symlink() || md.uid() != 0 || md.mode() & 0o022 != 0 {
            return Err(Error::Denied("release source or state directory is writable by another principal"));
        }
        if current == Path::new("/") { return Ok(()); }
        current = current.parent().ok_or(Error::Storage("invalid release path parent"))?;
    }
}

async fn run(program: &Path, args: &[String], timeout: Duration) -> Result<String, Error> {
    if !program.is_absolute() { return Err(Error::Configuration("release binary path must be absolute")); }
    let mut command = Command::new(program);
    command.args(args).env_clear().env("PATH", "/run/current-system/sw/bin:/usr/bin:/bin")
        .env("LANG", "C").env("HOME", "/var/empty")
        .stdin(Stdio::null()).stdout(Stdio::piped()).stderr(Stdio::null())
        .kill_on_drop(true);
    command.as_std_mut().process_group(0);
    let mut child = command.spawn().map_err(|_| Error::Unavailable("release program cannot start"))?;
    let pid = child.id().ok_or(Error::Unavailable("release process identity missing"))?;
    let future = async {
        let mut output = Vec::new();
        let stdout = child.stdout.take().ok_or(Error::Unavailable("release output unavailable"))?;
        stdout.take((MAX_RECORD + 1) as u64).read_to_end(&mut output).await
            .map_err(|_| Error::Unavailable("release output failed"))?;
        if output.len() > MAX_RECORD {
            return Err(Error::Protocol("release command output exceeds bound"));
        }
        let status = child.wait().await.map_err(|_| Error::Unavailable("release wait failed"))?;
        if !status.success() { return Err(Error::Unavailable("release command failed")); }
        String::from_utf8(output).map_err(|_| Error::Protocol("release command output not UTF-8"))
    };
    let result = tokio::time::timeout(timeout, future).await
        .map_err(|_| Error::Unavailable("release command deadline exceeded"))
        .and_then(|r| r);
    if result.is_err() {
        // Child processes of Nix and activation scripts must not survive cancellation.
        unsafe { libc::kill(-(pid as i32), libc::SIGKILL); }
    }
    result
}

pub struct ReleaseController {
    policy: Arc<Policy>,
    operation: Mutex<()>,
}
impl ReleaseController {
    pub fn new(policy: Arc<Policy>) -> Self {
        Self { policy, operation: Mutex::new(()) }
    }

    fn state_lock(&self) -> Result<File, Error> {
        require_root_owned_path(&self.policy.state_dir)?;
        let path = self.policy.state_dir.join(".lock");
        let file = OpenOptions::new().read(true).write(true).create(true)
            .mode(0o600).custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(path).map_err(|_| Error::Storage("release journal lock unavailable"))?;
        let md = file.metadata().map_err(|_| Error::Storage("release lock metadata unavailable"))?;
        if !md.is_file() || md.mode() & 0o077 != 0 || md.uid() != unsafe { libc::geteuid() } {
            return Err(Error::Storage("release journal lock is unsafe"));
        }
        file.try_lock_exclusive().map_err(|_| Error::Storage("release journal is already owned"))?;
        Ok(file)
    }

    fn read<T: for<'a> Deserialize<'a>>(&self, name: &str) -> Result<T, Error> {
        let path = self.policy.state_dir.join(name);
        let mut file = OpenOptions::new().read(true)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC).open(path)
            .map_err(|_| Error::Storage("release record missing"))?;
        let md = file.metadata().map_err(|_| Error::Storage("release metadata unavailable"))?;
        if !md.is_file() || md.mode() & 0o077 != 0 || md.uid() != unsafe { libc::geteuid() }
            || md.len() > MAX_RECORD as u64 {
            return Err(Error::Storage("unsafe release record"));
        }
        let mut content = Vec::new();
        file.take((MAX_RECORD + 1) as u64).read_to_end(&mut content)
            .map_err(|_| Error::Storage("release record cannot be read"))?;
        if content.len() > MAX_RECORD { return Err(Error::Storage("release record exceeds limit")); }
        serde_json::from_slice(&content).map_err(|_| Error::Storage("invalid release record"))
    }

    fn optional_journal(&self) -> Result<Option<Journal>, Error> {
        match fs::symlink_metadata(self.policy.state_dir.join("journal.json")) {
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(_) => Err(Error::Storage("cannot inspect release journal")),
            Ok(_) => self.read("journal.json").map(Some),
        }
    }

    fn atomic<T: Serialize>(&self, name: &str, record: &T) -> Result<(), Error> {
        let content = serde_json::to_vec(record).map_err(|_| Error::Storage("cannot encode release record"))?;
        if content.len() > MAX_RECORD || name.contains('/') || name.contains("..") {
            return Err(Error::Storage("invalid release record target"));
        }
        let temporary = self.policy.state_dir.join(format!(".record-{}", random_hex::<16>()?));
        let result = (|| {
            let mut file = OpenOptions::new().write(true).create_new(true).mode(0o600)
                .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
                .open(&temporary).map_err(|_| Error::Storage("temporary release record unavailable"))?;
            file.write_all(&content).and_then(|_| file.write_all(b"\n"))
                .and_then(|_| file.sync_all())
                .map_err(|_| Error::Storage("release record write failed"))?;
            fs::rename(&temporary, self.policy.state_dir.join(name))
                .map_err(|_| Error::Storage("release record replacement failed"))?;
            File::open(&self.policy.state_dir).and_then(|file| file.sync_all())
                .map_err(|_| Error::Storage("release record directory sync failed"))
        })();
        let _ = fs::remove_file(temporary);
        result
    }

    fn load_stage(&self, id: &str) -> Result<Manifest, Error> {
        if !valid_hex(id, 48) { return Err(Error::Denied("invalid release identifier")); }
        let record: Manifest = self.read(&format!("stage-{id}.json"))?;
        if record.id != id || record.host != self.policy.host ||
            !valid_hex(&record.revision, 40) || !store_path(&record.system) {
            return Err(Error::Storage("staged generation is invalid"));
        }
        Ok(record)
    }

    fn current_system(&self) -> Result<String, Error> {
        let path = fs::canonicalize(PROFILE)
            .map_err(|_| Error::Storage("current system generation unavailable"))?;
        let value = path.to_str().ok_or(Error::Storage("system path not UTF-8"))?.to_owned();
        if !store_path(&value) { return Err(Error::Storage("current generation is not a Nix store path")); }
        Ok(value)
    }

    async fn set_generation(&self, target: &str) -> Result<(), Error> {
        if !store_path(target) { return Err(Error::Denied("invalid generation target")); }
        run(&self.policy.nix_env, &[
            "-p".into(), PROFILE.into(), "--set".into(), target.into()
        ], Duration::from_secs(90)).await?;
        run(&Path::new(target).join(SWITCH), &["switch".into()],
            Duration::from_secs(90)).await?;
        Ok(())
    }
    async fn health(&self) -> Result<(), Error> {
        for service in &self.policy.health {
            let state = run(&self.policy.systemctl, &["is-active".into(), service.clone()],
                Duration::from_secs(15)).await?;
            if state.trim() != "active" {
                return Err(Error::Unavailable("release health service is not active"));
            }
        }
        Ok(())
    }

    async fn rollback(&self, mut txn: Journal) -> Result<(), Error> {
        let outcome = async {
            self.set_generation(&txn.previous).await?;
            self.health().await
        }.await;
        if outcome.is_err() {
            txn.phase = "rollback-failed".into();
            self.atomic("journal.json", &txn)?;
            return outcome;
        }
        txn.phase = "rolled-back".into();
        self.atomic("journal.json", &txn)
    }

    pub async fn stage(&self, revision: &str) -> Result<Manifest, Error> {
        if !valid_hex(revision, 40) { return Err(Error::Denied("exact Git revision required")); }
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        require_root_owned_path(&self.policy.source)?;
        if self.optional_journal()?.is_some_and(|j| j.interrupted()) {
            return Err(Error::Denied("incomplete release requires recovery"));
        }
        let root = self.policy.source.to_str().ok_or(Error::Configuration("source path must be UTF-8"))?;
        let head = run(&self.policy.git, &["-C".into(),root.into(),"rev-parse".into(),"HEAD".into()],
            Duration::from_secs(20)).await?;
        if head.trim() != revision { return Err(Error::Denied("source revision differs from requested commit")); }
        let changes = run(&self.policy.git, &["-C".into(),root.into(),"status".into(),"--porcelain".into()],
            Duration::from_secs(20)).await?;
        if !changes.trim().is_empty() { return Err(Error::Denied("source worktree is not clean")); }
        let attribute = format!("{root}#nixosConfigurations.{}.config.system.build.toplevel", self.policy.host);
        let system = run(&self.policy.nix, &[
            "build".into(), "--offline".into(), "--no-link".into(),
            "--print-out-paths".into(), attribute
        ], Duration::from_secs(7200)).await?;
        let system = system.trim();
        if !store_path(system) { return Err(Error::Protocol("Nix returned an invalid store path")); }
        let result = Manifest { id: random_hex::<24>()?, host: self.policy.host.clone(),
            revision: revision.into(), system: system.into(), created: Utc::now() };
        self.atomic(&format!("stage-{}.json",result.id), &result)?;
        Ok(result)
    }

    pub async fn verify_stage(&self, id: &str) -> Result<(), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        self.load_stage(id).map(|_| ())
    }

    pub async fn activate(&self, id: &str) -> Result<(), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let manifest = self.load_stage(id)?;
        if self.optional_journal()?.is_some_and(|j| j.interrupted()) {
            return Err(Error::Denied("interrupted release must be recovered first"));
        }
        let previous = self.current_system()?;
        if previous == manifest.system { return Err(Error::Denied("generation is already active")); }
        let mut txn = Journal { phase: "activating".into(), id: id.into(), previous,
            target: manifest.system.clone(), time: Utc::now() };
        self.atomic("journal.json", &txn)?;
        let attempt = async {
            self.set_generation(&manifest.system).await?;
            self.health().await
        }.await;
        if let Err(reason) = attempt {
            self.rollback(txn).await?;
            return Err(reason);
        }
        txn.phase = "active".into();
        self.atomic("journal.json", &txn)
    }

    pub async fn revert(&self, id: &str) -> Result<(), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let mut txn: Journal = self.read("journal.json")?;
        if txn.phase != "active" || txn.id != id || !store_path(&txn.target) ||
            !store_path(&txn.previous) || self.current_system()? != txn.target {
            return Err(Error::Denied("release journal does not match active generation"));
        }
        txn.phase = "reverting".into();
        self.atomic("journal.json", &txn)?;
        self.rollback(txn).await
    }

    /// Only a separate local, root-controlled recovery command calls this path.
    pub async fn recover(&self) -> Result<(), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let txn: Journal = self.read("journal.json")?;
        if !txn.interrupted() || !store_path(&txn.previous) {
            return Err(Error::Denied("no trusted interrupted release to recover"));
        }
        self.rollback(txn).await
    }

    pub async fn status(&self) -> Result<Journal, Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let journal: Journal = self.read("journal.json")?;
        if !valid_hex(&journal.id, 48) || !store_path(&journal.target)
            || !store_path(&journal.previous) ||
            !matches!(journal.phase.as_str(),"active"|"rolled-back"|"activating"|"reverting"|"rollback-failed") {
            return Err(Error::Storage("release journal is invalid"));
        }
        Ok(journal)
    }
}
