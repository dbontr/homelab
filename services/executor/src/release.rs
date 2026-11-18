//! Locked, source-pinned NixOS generation staging and rollback.
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::os::unix::process::CommandExt;
use std::path::Path;
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
        matches!(
            self.phase.as_str(),
            "activating" | "reverting" | "rollback-failed"
        )
    }
}

// An interrupted transaction can be observed before the switch is known to
// have completed. Only terminal receipts assert a specific live generation.
// The exact stage ID remains spent even after another release replaces the
// active journal. Files are inspected under the independent release lock.
fn stage_was_attempted(state: &Path, id: &str) -> Result<bool, Error> {
    if !valid_hex(id, 48) {
        return Err(Error::Denied("invalid release identifier"));
    }
    match fs::symlink_metadata(state.join(format!("attempt-{id}.json"))) {
        Ok(_) => Ok(true),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(false),
        Err(_) => Err(Error::Storage("release attempt record cannot be inspected")),
    }
}

fn journal_matches_generation(journal: &Journal, current: &str) -> bool {
    match journal.phase.as_str() {
        "active" => current == journal.target.as_str(),
        "rolled-back" => current == journal.previous.as_str(),
        _ => true,
    }
}

fn store_path(path: &str) -> bool {
    path.len() < 512
        && path.starts_with("/nix/store/")
        && !path.bytes().any(|b| b == 0 || b.is_ascii_whitespace())
        && !Path::new(path).components().any(|c| {
            matches!(
                c,
                std::path::Component::ParentDir | std::path::Component::CurDir
            )
        })
}

// Check the generated, immutable NixOS unit files before recording any release
// transaction. Never open or serialize credential contents; only inspect source
// existence, ownership and permissions.
fn verify_candidate_credentials(system: &Path, expected_uid: u32) -> Result<(), Error> {
    for service in ["homelab-securityd", "homelab-executord"] {
        let unit = system
            .join("etc/systemd/system")
            .join(format!("{service}.service"));
        let metadata = fs::metadata(&unit)
            .map_err(|_| Error::Denied("candidate protected unit is unavailable"))?;
        if !metadata.is_file() || metadata.len() > 64 << 10 {
            return Err(Error::Denied("candidate protected unit is invalid"));
        }
        let unit_text = fs::read_to_string(&unit)
            .map_err(|_| Error::Denied("cannot inspect candidate unit"))?;
        for line in unit_text.lines() {
            let Some(entries) = line.strip_prefix("LoadCredential=") else {
                continue;
            };
            if entries.trim().is_empty() {
                return Err(Error::Denied("empty candidate credential mapping"));
            }
            for entry in entries.split_ascii_whitespace() {
                let (name, source) = entry
                    .split_once(':')
                    .ok_or(Error::Denied("invalid candidate credential mapping"))?;
                if name.is_empty()
                    || !name
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.'))
                    || source.len() > 4096
                {
                    return Err(Error::Denied("invalid candidate credential mapping"));
                }
                let path = Path::new(source);
                if !path.is_absolute()
                    || path.components().any(|part| {
                        matches!(
                            part,
                            std::path::Component::ParentDir | std::path::Component::CurDir
                        )
                    })
                {
                    return Err(Error::Denied("candidate credential source is not absolute"));
                }
                let file = fs::symlink_metadata(path)
                    .map_err(|_| Error::Denied("candidate protected credential missing"))?;
                if !file.is_file()
                    || file.uid() != expected_uid
                    || file.len() == 0
                    || file.mode() & 0o037 != 0
                    || file.mode() & 0o400 == 0
                {
                    return Err(Error::Denied("candidate protected credential is unsafe"));
                }
            }
        }
    }
    Ok(())
}

// Select the committed Git tree, not the live filesystem checkout. Escaping
// the file URL prevents path characters from changing the Nix flake reference.
fn pinned_flake(source: &Path, revision: &str, host: &str) -> Result<String, Error> {
    if !valid_hex(revision, 40) || !crate::valid_host(host) {
        return Err(Error::Denied("invalid pinned release revision or host"));
    }
    let file_url = url::Url::from_directory_path(source)
        .map_err(|_| Error::Configuration("release source must be an absolute file path"))?;
    let source_path = file_url
        .as_str()
        .strip_prefix("file://")
        .ok_or(Error::Configuration("invalid release source file URL"))?;
    Ok(format!(
        "git+file://{source_path}?rev={revision}#nixosConfigurations.{host}.config.system.build.toplevel"
    ))
}

fn require_root_owned_path(path: &Path) -> Result<(), Error> {
    if !path.is_absolute() {
        return Err(Error::Configuration(
            "trusted release path must be absolute",
        ));
    }
    let mut current = path;
    loop {
        let md = fs::symlink_metadata(current)
            .map_err(|_| Error::Storage("trusted release path unavailable"))?;
        if md.file_type().is_symlink() || md.uid() != 0 || md.mode() & 0o022 != 0 {
            return Err(Error::Denied(
                "release source or state directory is writable by another principal",
            ));
        }
        if current == Path::new("/") {
            return Ok(());
        }
        current = current
            .parent()
            .ok_or(Error::Storage("invalid release path parent"))?;
    }
}

async fn run(program: &Path, args: &[String], timeout: Duration) -> Result<String, Error> {
    if !program.is_absolute() {
        return Err(Error::Configuration("release binary path must be absolute"));
    }
    let mut command = Command::new(program);
    command
        .args(args)
        .env_clear()
        .env("PATH", "/run/current-system/sw/bin:/usr/bin:/bin")
        .env("LANG", "C")
        .env("HOME", "/var/empty")
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .kill_on_drop(true);
    command.as_std_mut().process_group(0);
    let mut child = command
        .spawn()
        .map_err(|_| Error::Unavailable("release program cannot start"))?;
    let pid = child
        .id()
        .ok_or(Error::Unavailable("release process identity missing"))?;
    let future = async {
        let mut output = Vec::new();
        let stdout = child
            .stdout
            .take()
            .ok_or(Error::Unavailable("release output unavailable"))?;
        stdout
            .take((MAX_RECORD + 1) as u64)
            .read_to_end(&mut output)
            .await
            .map_err(|_| Error::Unavailable("release output failed"))?;
        if output.len() > MAX_RECORD {
            return Err(Error::Protocol("release command output exceeds bound"));
        }
        let status = child
            .wait()
            .await
            .map_err(|_| Error::Unavailable("release wait failed"))?;
        if !status.success() {
            return Err(Error::Unavailable("release command failed"));
        }
        String::from_utf8(output).map_err(|_| Error::Protocol("release command output not UTF-8"))
    };
    let result = tokio::time::timeout(timeout, future)
        .await
        .map_err(|_| Error::Unavailable("release command deadline exceeded"))
        .and_then(|r| r);
    if result.is_err() {
        // Child processes of Nix and activation scripts must not survive cancellation.
        unsafe {
            libc::kill(-(pid as i32), libc::SIGKILL);
        }
    }
    result
}

pub struct ReleaseController {
    policy: Arc<Policy>,
    operation: Mutex<()>,
}
impl ReleaseController {
    pub fn new(policy: Arc<Policy>) -> Self {
        Self {
            policy,
            operation: Mutex::new(()),
        }
    }

    fn state_lock(&self) -> Result<File, Error> {
        require_root_owned_path(&self.policy.state_dir)?;
        let path = self.policy.state_dir.join(".lock");
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(path)
            .map_err(|_| Error::Storage("release journal lock unavailable"))?;
        let md = file
            .metadata()
            .map_err(|_| Error::Storage("release lock metadata unavailable"))?;
        if !md.is_file() || md.mode() & 0o077 != 0 || md.uid() != unsafe { libc::geteuid() } {
            return Err(Error::Storage("release journal lock is unsafe"));
        }
        file.try_lock_exclusive()
            .map_err(|_| Error::Storage("release journal is already owned"))?;
        Ok(file)
    }

    fn read<T: for<'a> Deserialize<'a>>(&self, name: &str) -> Result<T, Error> {
        let path = self.policy.state_dir.join(name);
        let file = OpenOptions::new()
            .read(true)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(path)
            .map_err(|_| Error::Storage("release record missing"))?;
        let md = file
            .metadata()
            .map_err(|_| Error::Storage("release metadata unavailable"))?;
        if !md.is_file()
            || md.mode() & 0o077 != 0
            || md.uid() != unsafe { libc::geteuid() }
            || md.len() > MAX_RECORD as u64
        {
            return Err(Error::Storage("unsafe release record"));
        }
        let mut content = Vec::new();
        file.take((MAX_RECORD + 1) as u64)
            .read_to_end(&mut content)
            .map_err(|_| Error::Storage("release record cannot be read"))?;
        if content.len() > MAX_RECORD {
            return Err(Error::Storage("release record exceeds limit"));
        }
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
        let content = serde_json::to_vec(record)
            .map_err(|_| Error::Storage("cannot encode release record"))?;
        if content.len() > MAX_RECORD || name.contains('/') || name.contains("..") {
            return Err(Error::Storage("invalid release record target"));
        }
        let temporary = self
            .policy
            .state_dir
            .join(format!(".record-{}", random_hex::<16>()?));
        let result = (|| {
            let mut file = OpenOptions::new()
                .write(true)
                .create_new(true)
                .mode(0o600)
                .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
                .open(&temporary)
                .map_err(|_| Error::Storage("temporary release record unavailable"))?;
            file.write_all(&content)
                .and_then(|_| file.write_all(b"\n"))
                .and_then(|_| file.sync_all())
                .map_err(|_| Error::Storage("release record write failed"))?;
            fs::rename(&temporary, self.policy.state_dir.join(name))
                .map_err(|_| Error::Storage("release record replacement failed"))?;
            File::open(&self.policy.state_dir)
                .and_then(|file| file.sync_all())
                .map_err(|_| Error::Storage("release record directory sync failed"))
        })();
        let _ = fs::remove_file(temporary);
        result
    }

    fn load_stage(&self, id: &str) -> Result<Manifest, Error> {
        if !valid_hex(id, 48) {
            return Err(Error::Denied("invalid release identifier"));
        }
        let record: Manifest = self.read(&format!("stage-{id}.json"))?;
        if record.id != id
            || record.host != self.policy.host
            || !valid_hex(&record.revision, 40)
            || !store_path(&record.system)
        {
            return Err(Error::Storage("staged generation is invalid"));
        }
        Ok(record)
    }

    fn current_system(&self) -> Result<String, Error> {
        let path = fs::canonicalize(PROFILE)
            .map_err(|_| Error::Storage("current system generation unavailable"))?;
        let value = path
            .to_str()
            .ok_or(Error::Storage("system path not UTF-8"))?
            .to_owned();
        if !store_path(&value) {
            return Err(Error::Storage("current generation is not a Nix store path"));
        }
        Ok(value)
    }

    async fn set_generation(&self, target: &str) -> Result<(), Error> {
        if !store_path(target) {
            return Err(Error::Denied("invalid generation target"));
        }
        run(
            &self.policy.nix_env,
            &["-p".into(), PROFILE.into(), "--set".into(), target.into()],
            Duration::from_secs(90),
        )
        .await?;
        run(
            &Path::new(target).join(SWITCH),
            &["switch".into()],
            Duration::from_secs(90),
        )
        .await?;
        Ok(())
    }
    async fn health(&self) -> Result<(), Error> {
        for service in &self.policy.health {
            let state = run(
                &self.policy.systemctl,
                &["is-active".into(), service.clone()],
                Duration::from_secs(15),
            )
            .await?;
            if state.trim() != "active" {
                return Err(Error::Unavailable("release health service is not active"));
            }
        }
        // These destinations are parsed, deduplicated and constrained to
        // literal private IPs by the immutable Nix executor policy.
        for address in &self.policy.health_sockets {
            match tokio::time::timeout(
                Duration::from_secs(3),
                tokio::net::TcpStream::connect(*address),
            )
            .await
            {
                Ok(Ok(connection)) => drop(connection),
                _ => return Err(Error::Unavailable("protected release TCP health check failed")),
            }
        }
        Ok(())
    }

    async fn rollback(&self, mut txn: Journal) -> Result<(), Error> {
        let outcome = async {
            self.set_generation(&txn.previous).await?;
            self.health().await
        }
        .await;
        if outcome.is_err() {
            txn.phase = "rollback-failed".into();
            self.atomic("journal.json", &txn)?;
            return outcome;
        }
        txn.phase = "rolled-back".into();
        self.atomic("journal.json", &txn)
    }

    pub async fn stage(&self, revision: &str) -> Result<Manifest, Error> {
        if !valid_hex(revision, 40) {
            return Err(Error::Denied("exact Git revision required"));
        }
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        require_root_owned_path(&self.policy.source)?;
        if self.optional_journal()?.is_some_and(|j| j.interrupted()) {
            return Err(Error::Denied("incomplete release requires recovery"));
        }
        let root = self
            .policy
            .source
            .to_str()
            .ok_or(Error::Configuration("source path must be UTF-8"))?;
        let head = run(
            &self.policy.git,
            &["-C".into(), root.into(), "rev-parse".into(), "HEAD".into()],
            Duration::from_secs(20),
        )
        .await?;
        if head.trim() != revision {
            return Err(Error::Denied(
                "source revision differs from requested commit",
            ));
        }
        let changes = run(
            &self.policy.git,
            &[
                "-C".into(),
                root.into(),
                "status".into(),
                "--porcelain".into(),
                "--untracked-files=all".into(),
            ],
            Duration::from_secs(20),
        )
        .await?;
        if !changes.trim().is_empty() {
            return Err(Error::Denied("source worktree is not clean"));
        }
        let attribute = pinned_flake(&self.policy.source, revision, &self.policy.host)?;
        let system = run(
            &self.policy.nix,
            &[
                "build".into(),
                "--offline".into(),
                "--no-link".into(),
                "--print-out-paths".into(),
                attribute,
            ],
            Duration::from_secs(7200),
        )
        .await?;
        let system = system.trim();
        if !store_path(system) {
            return Err(Error::Protocol("Nix returned an invalid store path"));
        }
        let result = Manifest {
            id: random_hex::<24>()?,
            host: self.policy.host.clone(),
            revision: revision.into(),
            system: system.into(),
            created: Utc::now(),
        };
        self.atomic(&format!("stage-{}.json", result.id), &result)?;
        Ok(result)
    }

    pub async fn verify_stage(&self, id: &str) -> Result<(), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let manifest = self.load_stage(id)?;
        verify_candidate_credentials(Path::new(&manifest.system), 0)
    }

    pub async fn activate(&self, id: &str) -> Result<(), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let manifest = self.load_stage(id)?;
        if stage_was_attempted(&self.policy.state_dir, id)? {
            return Err(Error::Denied("staged release already attempted"));
        }
        if let Some(previous_attempt) = self.optional_journal()? {
            if previous_attempt.interrupted() {
                return Err(Error::Denied("interrupted release must be recovered first"));
            }
            if previous_attempt.id == id {
                return Err(Error::Denied("staged release already attempted"));
            }
        }
        let previous = self.current_system()?;
        if previous == manifest.system {
            return Err(Error::Denied("generation is already active"));
        }
        verify_candidate_credentials(Path::new(&manifest.system), 0)?;
        let mut txn = Journal {
            phase: "activating".into(),
            id: id.into(),
            previous,
            target: manifest.system.clone(),
            time: Utc::now(),
        };
        // Persist the attempt before switching any host generation.
        self.atomic(&format!("attempt-{id}.json"), &txn)?;
        self.atomic("journal.json", &txn)?;
        let attempt = async {
            self.set_generation(&manifest.system).await?;
            self.health().await
        }
        .await;
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
        if txn.phase != "active"
            || txn.id != id
            || !store_path(&txn.target)
            || !store_path(&txn.previous)
            || self.current_system()? != txn.target
        {
            return Err(Error::Denied(
                "release journal does not match active generation",
            ));
        }
        txn.phase = "reverting".into();
        self.atomic("journal.json", &txn)?;
        self.rollback(txn).await
    }

    /// Only a separate local, root-controlled recovery command calls this path.
    pub async fn recover(&self) -> Result<(), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let mut txn: Journal = self.read("journal.json")?;
        if !txn.interrupted() || !store_path(&txn.previous) {
            return Err(Error::Denied("no trusted interrupted release to recover"));
        }
        // If the rollback switch completed before a crash, verify the restored
        // services without switching the same generation a second time.
        if self.current_system()? == txn.previous {
            self.health().await?;
            txn.phase = "rolled-back".into();
            return self.atomic("journal.json", &txn);
        }
        self.rollback(txn).await
    }

    pub async fn status(&self) -> Result<(Journal, Manifest), Error> {
        let _guard = self.operation.lock().await;
        let _file = self.state_lock()?;
        let journal: Journal = self.read("journal.json")?;
        if !valid_hex(&journal.id, 48)
            || !store_path(&journal.target)
            || !store_path(&journal.previous)
            || !matches!(
                journal.phase.as_str(),
                "active" | "rolled-back" | "activating" | "reverting" | "rollback-failed"
            )
        {
            return Err(Error::Storage("release journal is invalid"));
        }
        if matches!(journal.phase.as_str(), "active" | "rolled-back")
            && !journal_matches_generation(&journal, &self.current_system()?)
        {
            return Err(Error::Storage("release journal differs from live generation"));
        }
        let manifest = self.load_stage(&journal.id)?;
        if manifest.system != journal.target {
            return Err(Error::Storage("release journal target differs from staged generation"));
        }
        Ok((journal, manifest))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::{symlink, PermissionsExt};

    #[test]
    fn candidate_credentials_are_verified_before_release_mutation() {
        let system = std::env::temp_dir().join(format!(
            "homelab-release-credential-{}-{}",
            std::process::id(),
            random_hex::<8>().unwrap()
        ));
        let unit_dir = system.join("etc/systemd/system");
        fs::create_dir_all(&unit_dir).unwrap();
        let security = unit_dir.join("homelab-securityd.service");
        let executor = unit_dir.join("homelab-executord.service");
        fs::write(&security, b"[Service]\n").unwrap();

        let credential = system.join("owner-only-token");
        fs::write(&credential, b"fixture-only-not-a-credential").unwrap();
        let set_mode = |mode| {
            fs::set_permissions(&credential, fs::Permissions::from_mode(mode)).unwrap();
        };
        set_mode(0o600);
        let mapping = format!(
            "[Service]\nLoadCredential=workspace-token:{}\n",
            credential.display()
        );
        fs::write(&executor, &mapping).unwrap();

        // Tests may run without root, but actual release entrypoints require UID 0.
        let owner = unsafe { libc::geteuid() };
        assert!(verify_candidate_credentials(&system, owner).is_ok());
        set_mode(0o640);
        assert!(verify_candidate_credentials(&system, owner).is_ok());

        set_mode(0o644);
        assert!(verify_candidate_credentials(&system, owner).is_err());
        set_mode(0o660);
        assert!(verify_candidate_credentials(&system, owner).is_err());
        set_mode(0o600);
        assert!(verify_candidate_credentials(&system, owner + 1).is_err());

        fs::remove_file(&credential).unwrap();
        assert!(verify_candidate_credentials(&system, owner).is_err());
        fs::write(&credential, b"fixture-only-not-a-credential").unwrap();
        set_mode(0o600);
        let linked = system.join("linked-token");
        symlink(&credential, &linked).unwrap();
        fs::write(
            &executor,
            format!(
                "[Service]\nLoadCredential=workspace-token:{}\n",
                linked.display()
            ),
        )
        .unwrap();
        assert!(verify_candidate_credentials(&system, owner).is_err());

        fs::write(
            &executor,
            b"[Service]\nLoadCredential=workspace-token:relative\n",
        )
        .unwrap();
        assert!(verify_candidate_credentials(&system, owner).is_err());
        fs::write(&executor, b"[Service]\nLoadCredential=workspace-token:\n").unwrap();
        assert!(verify_candidate_credentials(&system, owner).is_err());
        fs::write(&executor, b"[Service]\nLoadCredential=   \n").unwrap();
        assert!(verify_candidate_credentials(&system, owner).is_err());
        fs::write(&executor, mapping).unwrap();
        assert!(verify_candidate_credentials(&system, owner).is_ok());

        fs::remove_dir_all(&system).unwrap();
    }

    #[test]
    fn release_receipts_reject_inconsistent_live_generations() {
        let mut journal = Journal {
            phase: "active".into(),
            id: "a".repeat(48),
            previous: "/nix/store/prior-system".into(),
            target: "/nix/store/target-system".into(),
            time: Utc::now(),
        };
        assert!(journal_matches_generation(&journal, &journal.target));
        assert!(!journal_matches_generation(&journal, &journal.previous));
        journal.phase = "rolled-back".into();
        assert!(journal_matches_generation(&journal, &journal.previous));
        assert!(!journal_matches_generation(&journal, &journal.target));
        journal.phase = "activating".into();
        assert!(journal_matches_generation(&journal, &journal.previous));
    }

    #[test]
    fn stage_reuse_stays_denied_after_journal_replacement() {
        let root = std::env::temp_dir().join(format!(
            "homelab-stage-fence-{}",
            random_hex::<16>().unwrap()
        ));
        fs::create_dir(&root).unwrap();
        let first = "a".repeat(48);
        let second = "b".repeat(48);
        assert!(!stage_was_attempted(&root, &first).unwrap());
        fs::write(root.join(format!("attempt-{first}.json")), b"{}").unwrap();
        fs::write(root.join("journal.json"), format!("{{\\"id\\":\\"{second}\\"}}")).unwrap();
        assert!(stage_was_attempted(&root, &first).unwrap());
        assert!(!stage_was_attempted(&root, &second).unwrap());
        assert!(stage_was_attempted(&root, "not-a-release-id").is_err());
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn released_generation_identifiers_are_not_mutable_refs() {
        assert!(store_path("/nix/store/0123456789abcdef-generation"));
        for path in [
            "/nix/store/../profiles/system",
            "/nix/store/unstable path",
            "/tmp/untrusted",
        ] {
            assert!(!store_path(path));
        }
    }

    #[test]
    fn release_flake_selects_an_exact_git_commit() {
        let revision = "a".repeat(40);
        let reference = pinned_flake(Path::new("/srv/homelab"), &revision, "saturn").unwrap();
        assert!(reference.starts_with("git+file:///srv/homelab"));
        assert_eq!(
            reference.split_once('?').unwrap().1,
            format!("rev={revision}#nixosConfigurations.saturn.config.system.build.toplevel")
        );
        assert!(!reference.starts_with("/srv/homelab#"));
    }

    #[test]
    fn release_flake_escapes_path_delimiters_and_rejects_unpinned_sources() {
        let revision = "0".repeat(40);
        let reference =
            pinned_flake(Path::new("/srv/lab #snapshot?"), &revision, "saturn").unwrap();
        assert!(reference.contains("lab%20%23snapshot%3F"));
        assert!(pinned_flake(Path::new("relative/repo"), &revision, "saturn").is_err());
        assert!(pinned_flake(Path::new("/srv/homelab"), "HEAD", "saturn").is_err());
        assert!(pinned_flake(Path::new("/srv/homelab"), &revision, "untrusted/host").is_err());
    }
}
