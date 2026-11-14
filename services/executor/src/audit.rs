//! Durable, inode-fenced privileged execution journal with exclusive state custody.
use std::collections::BTreeMap;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use chrono::{DateTime, Datelike, Utc};
use fs2::FileExt;
use serde::{Deserialize, Serialize};

use crate::{valid_hex, valid_host, Error};

const MAX_AUDIT_BYTES: u64 = 64 << 20;

fn owner_only(metadata: &fs::Metadata) -> bool {
    metadata.is_file()
        && metadata.nlink() == 1
        && metadata.mode() & 0o077 == 0
        && metadata.uid() == unsafe { libc::geteuid() }
}

fn private_directory(directory: &Path) -> Result<(), Error> {
    if !directory.is_absolute()
        || directory == Path::new("/")
        || directory
            .components()
            .any(|p| matches!(p, std::path::Component::ParentDir))
    {
        return Err(Error::Configuration(
            "private executor state directory required",
        ));
    }
    let metadata = fs::symlink_metadata(directory)
        .map_err(|_| Error::Storage("executor state directory unavailable"))?;
    if !metadata.is_dir()
        || metadata.file_type().is_symlink()
        || metadata.mode() & 0o077 != 0
        || metadata.uid() != unsafe { libc::geteuid() }
    {
        return Err(Error::Storage("executor state directory is not owner-only"));
    }
    Ok(())
}

/// This inode is deliberately never unlinked: Go and Rust daemons use the same lock.
pub struct StateLease {
    _file: File,
}
impl StateLease {
    pub fn acquire(directory: &Path) -> Result<Self, Error> {
        private_directory(directory)?;
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(directory.join(".executord.lock"))
            .map_err(|_| Error::Storage("executor state lock unavailable"))?;
        if !owner_only(
            &file
                .metadata()
                .map_err(|_| Error::Storage("lock metadata unavailable"))?,
        ) {
            return Err(Error::Storage("unsafe executor state lock"));
        }
        match file.try_lock_exclusive() {
            Ok(()) => {}
            Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                return Err(Error::Storage("another executor owns the state"));
            }
            Err(_) => return Err(Error::Storage("executor state lock unavailable")),
        }
        Ok(Self { _file: file })
    }
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Event {
    pub id: String,
    pub operation: String,
    pub target: String,
    pub arguments_sha256: String,
    pub outcome: String,
    pub at: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reconciliation_sha256: Option<String>,
}
impl Event {
    fn valid(&self) -> bool {
        valid_hex(&self.id, 32)
            && valid_hex(&self.arguments_sha256, 64)
            && !self.operation.is_empty()
            && self.operation.len() <= 128
            && valid_host(&self.target)
            && matches!(
                self.outcome.as_str(),
                "started" | "accepted" | "succeeded" | "failed" | "indeterminate" | "reconciled"
            )
            && (if self.outcome == "reconciled" {
                self.reconciliation_sha256
                    .as_deref()
                    .is_some_and(|digest| valid_hex(digest, 64))
            } else {
                self.reconciliation_sha256.is_none()
            })
            && DateTime::parse_from_rfc3339(&self.at).is_ok_and(|date| date.year() > 1)
    }
}

struct Journal {
    file: File,
    seen: BTreeMap<String, Event>,
    faulted: bool,
}
pub struct AuditJournal {
    path: PathBuf,
    dev: u64,
    ino: u64,
    inner: Mutex<Journal>,
    _lease: StateLease,
}

fn transition(seen: &BTreeMap<String, Event>, current: &Event) -> Result<(), Error> {
    let prior = seen.get(&current.id);
    if current.outcome == "started" {
        if prior.is_some() {
            return Err(Error::Storage("duplicate audit identity"));
        }
        return Ok(());
    }
    let prior = prior.ok_or(Error::Storage("audit outcome has no start event"))?;
    let before = DateTime::parse_from_rfc3339(&prior.at)
        .map_err(|_| Error::Storage("invalid audit time"))?;
    let after = DateTime::parse_from_rfc3339(&current.at)
        .map_err(|_| Error::Storage("invalid audit time"))?;
    let valid_prior = if current.outcome == "reconciled" {
        matches!(
            prior.outcome.as_str(),
            "started" | "indeterminate" | "failed"
        )
    } else {
        prior.outcome == "started"
    };
    if !valid_prior
        || prior.operation != current.operation
        || prior.target != current.target
        || prior.arguments_sha256 != current.arguments_sha256
        || after < before
    {
        return Err(Error::Storage("contradictory audit transition"));
    }
    Ok(())
}

impl AuditJournal {
    /// The service manager can restart executord while a trusted release CLI
    /// holds the exclusive lease. Wait before constructing the journal or
    /// opening any privileged transport. Unsafe files and unrelated I/O
    /// failures always fail closed without retrying.
    pub fn open_waiting_for_release(path: &Path, maximum_wait: Duration) -> Result<Self, Error> {
        let started = Instant::now();
        loop {
            match Self::open(path) {
                Ok(journal) => return Ok(journal),
                Err(Error::Storage("another executor owns the state"))
                    if started.elapsed() < maximum_wait =>
                {
                    std::thread::sleep(Duration::from_millis(100));
                }
                Err(error) => return Err(error),
            }
        }
    }

    pub fn open(path: &Path) -> Result<Self, Error> {
        let parent = path
            .parent()
            .ok_or(Error::Configuration("audit directory required"))?;
        let lease = StateLease::acquire(parent)?;
        let mut file = OpenOptions::new()
            .read(true)
            .append(true)
            .create(true)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(path)
            .map_err(|_| Error::Storage("audit journal unavailable"))?;
        let metadata = file
            .metadata()
            .map_err(|_| Error::Storage("audit metadata unavailable"))?;
        if !owner_only(&metadata) || metadata.len() > MAX_AUDIT_BYTES {
            return Err(Error::Storage("audit journal custody or size is unsafe"));
        }
        File::open(parent)
            .and_then(|f| f.sync_all())
            .map_err(|_| Error::Storage("audit inode cannot be synchronized"))?;
        file.seek(SeekFrom::Start(0))
            .map_err(|_| Error::Storage("audit seek failed"))?;
        let mut contents = Vec::new();
        std::io::Read::by_ref(&mut file)
            .take(MAX_AUDIT_BYTES + 1)
            .read_to_end(&mut contents)
            .map_err(|_| Error::Storage("audit read failed"))?;
        if contents.len() as u64 > MAX_AUDIT_BYTES
            || (!contents.is_empty() && contents.last() != Some(&b'\n'))
        {
            return Err(Error::Storage("audit journal has a partial record"));
        }
        let mut seen = BTreeMap::new();
        let record_data = if contents.is_empty() {
            &contents[..]
        } else {
            &contents[..contents.len() - 1]
        };
        for line in record_data.split(|b| *b == b'\n') {
            if contents.is_empty() {
                break;
            }
            if line.is_empty() {
                return Err(Error::Storage("audit contains an empty record"));
            }
            if line.len() > 2048 {
                return Err(Error::Storage("audit record exceeds limit"));
            }
            let entry: Event =
                serde_json::from_slice(line).map_err(|_| Error::Storage("invalid audit record"))?;
            if !entry.valid() {
                return Err(Error::Storage("invalid audit record fields"));
            }
            transition(&seen, &entry)?;
            seen.insert(entry.id.clone(), entry);
        }
        Ok(Self {
            path: path.into(),
            dev: metadata.dev(),
            ino: metadata.ino(),
            inner: Mutex::new(Journal {
                file,
                seen,
                faulted: false,
            }),
            _lease: lease,
        })
    }

    /// An uncertain append or directory replacement permanently disables execution.
    /// Return operations with no confirmed effect. Both unfinished and
    /// indeterminate attempts require explicit reconciliation before retry.
    pub fn unresolved(&self) -> Result<Vec<Event>, Error> {
        let journal = self.inner.lock().unwrap_or_else(|_| std::process::abort());
        if journal.faulted {
            return Err(Error::Storage("audit journal is in an uncertain state"));
        }
        let path_meta =
            fs::symlink_metadata(&self.path).map_err(|_| Error::Storage("audit path missing"))?;
        let fd_meta = journal
            .file
            .metadata()
            .map_err(|_| Error::Storage("audit descriptor unavailable"))?;
        if !owner_only(&fd_meta)
            || path_meta.dev() != self.dev
            || path_meta.ino() != self.ino
            || fd_meta.dev() != self.dev
            || fd_meta.ino() != self.ino
        {
            return Err(Error::Storage("audit journal was replaced"));
        }
        Ok(journal
            .seen
            .values()
            .filter(|event| {
                matches!(
                    event.outcome.as_str(),
                    "started" | "indeterminate" | "failed"
                )
            })
            .cloned()
            .collect())
    }

    /// Look up a caller request identity under the same protected journal
    /// lease. Identity reuse with a different operation, target or argument
    /// digest is rejected, including after an operator reconciliation.
    pub fn request_outcome(
        &self,
        id: &str,
        operation: &str,
        target: &str,
        digest: &str,
    ) -> Result<Option<String>, Error> {
        if !valid_hex(id, 32) || !valid_hex(digest, 64) {
            return Err(Error::Denied("invalid execution request identity"));
        }
        let journal = self.inner.lock().unwrap_or_else(|_| std::process::abort());
        if journal.faulted {
            return Err(Error::Storage("audit journal is in an uncertain state"));
        }
        let path_meta =
            fs::symlink_metadata(&self.path).map_err(|_| Error::Storage("audit path missing"))?;
        let fd_meta = journal
            .file
            .metadata()
            .map_err(|_| Error::Storage("audit descriptor unavailable"))?;
        if !owner_only(&fd_meta)
            || path_meta.dev() != self.dev
            || path_meta.ino() != self.ino
            || fd_meta.dev() != self.dev
            || fd_meta.ino() != self.ino
        {
            return Err(Error::Storage("audit journal was replaced"));
        }
        match journal.seen.get(id) {
            None => Ok(None),
            Some(previous)
                if previous.operation == operation
                    && previous.target == target
                    && previous.arguments_sha256 == digest =>
            {
                Ok(Some(previous.outcome.clone()))
            }
            Some(_) => Err(Error::Denied(
                "execution request identity used for different arguments",
            )),
        }
    }

    pub fn record(
        &self,
        id: &str,
        operation: &str,
        target: &str,
        digest: &str,
        outcome: &str,
    ) -> Result<(), Error> {
        let entry = Event {
            id: id.into(),
            operation: operation.into(),
            target: target.into(),
            arguments_sha256: digest.into(),
            outcome: outcome.into(),
            at: Utc::now().to_rfc3339(),
            reconciliation_sha256: None,
        };
        self.append_event(entry)
    }

    /// A trusted local recovery tool can append a resolution only after an
    /// operator records separate evidence for a started or uncertain attempt.
    pub fn reconcile(&self, id: &str, evidence_sha256: &str) -> Result<(), Error> {
        if !valid_hex(evidence_sha256, 64) {
            return Err(Error::Denied("invalid reconciliation evidence digest"));
        }
        let original = {
            let journal = self.inner.lock().unwrap_or_else(|_| std::process::abort());
            if journal.faulted {
                return Err(Error::Storage("audit journal is in an uncertain state"));
            }
            journal
                .seen
                .get(id)
                .cloned()
                .ok_or(Error::Denied("audit attempt not found"))?
        };
        if !matches!(
            original.outcome.as_str(),
            "started" | "indeterminate" | "failed"
        ) {
            return Err(Error::Denied("audit attempt is already resolved"));
        }
        self.append_event(Event {
            id: original.id,
            operation: original.operation,
            target: original.target,
            arguments_sha256: original.arguments_sha256,
            outcome: "reconciled".into(),
            at: Utc::now().to_rfc3339(),
            reconciliation_sha256: Some(evidence_sha256.into()),
        })
    }

    fn append_event(&self, entry: Event) -> Result<(), Error> {
        if !entry.valid() {
            return Err(Error::Denied("invalid audit event"));
        }
        let raw =
            serde_json::to_vec(&entry).map_err(|_| Error::Storage("audit encoding failed"))?;
        if raw.len() > 1024 {
            return Err(Error::Storage("audit record too large"));
        }
        let mut journal = self.inner.lock().unwrap_or_else(|_| std::process::abort());
        if journal.faulted {
            return Err(Error::Storage("audit journal is in an uncertain state"));
        }
        // Policy refusals are not journal I/O failures. A denied replay
        // leaves the journal usable for unrelated authorized operations.
        transition(&journal.seen, &entry)?;
        if entry.outcome == "started"
            && journal.seen.values().any(|prior| {
                prior.operation == entry.operation
                    && prior.target == entry.target
                    && prior.arguments_sha256 == entry.arguments_sha256
                    && matches!(
                        prior.outcome.as_str(),
                        "started" | "indeterminate" | "failed"
                    )
            })
        {
            return Err(Error::Denied(
                "matching unresolved execution requires reconciliation",
            ));
        }
        let result = (|| {
            let path_meta = fs::symlink_metadata(&self.path)
                .map_err(|_| Error::Storage("audit path missing"))?;
            let fd_meta = journal
                .file
                .metadata()
                .map_err(|_| Error::Storage("audit descriptor unavailable"))?;
            if !owner_only(&fd_meta)
                || path_meta.dev() != self.dev
                || path_meta.ino() != self.ino
                || fd_meta.dev() != self.dev
                || fd_meta.ino() != self.ino
            {
                return Err(Error::Storage("audit journal was replaced"));
            }
            if fd_meta.len() + raw.len() as u64 + 1 > MAX_AUDIT_BYTES {
                return Err(Error::Storage("audit journal reached retention limit"));
            }
            journal
                .file
                .write_all(&raw)
                .and_then(|_| journal.file.write_all(b"\n"))
                .and_then(|_| journal.file.sync_all())
                .map_err(|_| Error::Storage("durable audit append failed"))
        })();
        match result {
            Ok(()) => {
                journal.seen.insert(entry.id.clone(), entry);
                Ok(())
            }
            Err(e) => {
                journal.faulted = true;
                Err(e)
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    fn event(id: &str, outcome: &str) -> Event {
        Event {
            id: id.into(),
            operation: "infrastructure__service_restart".into(),
            target: "jupiter".into(),
            arguments_sha256: "a".repeat(64),
            outcome: outcome.into(),
            at: "2026-11-10T18:00:00Z".into(),
            reconciliation_sha256: None,
        }
    }

    #[test]
    fn rejects_shared_executor_journal_and_lease_inodes() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-hardlink-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let source = directory.join("shared");
        fs::write(&source, b"").unwrap();
        fs::set_permissions(&source, fs::Permissions::from_mode(0o600)).unwrap();
        let journal = directory.join("executor-actions.jsonl");
        fs::hard_link(&source, &journal).unwrap();
        assert!(AuditJournal::open(&journal).is_err());
        fs::remove_file(&journal).unwrap();
        fs::remove_file(directory.join(".executord.lock")).unwrap();
        fs::hard_link(&source, directory.join(".executord.lock")).unwrap();
        assert!(AuditJournal::open(&journal).is_err());
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn request_identity_replays_only_its_durable_exact_scope() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-request-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let id = "a".repeat(32);
        let digest = "b".repeat(64);
        {
            let journal = AuditJournal::open(&path).unwrap();
            assert!(journal
                .request_outcome(&id, "infrastructure__service_restart", "jupiter", &digest)
                .unwrap()
                .is_none());
            journal
                .record(
                    &id,
                    "infrastructure__service_restart",
                    "jupiter",
                    &digest,
                    "started",
                )
                .unwrap();
            assert_eq!(
                journal
                    .request_outcome(&id, "infrastructure__service_restart", "jupiter", &digest)
                    .unwrap()
                    .as_deref(),
                Some("started")
            );
            assert!(journal
                .request_outcome(&id, "infrastructure__service_restart", "venus", &digest)
                .is_err());
            assert!(journal
                .request_outcome(&id, "infrastructure__service_stop", "jupiter", &digest)
                .is_err());
            assert!(journal
                .request_outcome(
                    &id,
                    "infrastructure__service_restart",
                    "jupiter",
                    &"c".repeat(64)
                )
                .is_err());
            journal
                .record(
                    &id,
                    "infrastructure__service_restart",
                    "jupiter",
                    &digest,
                    "succeeded",
                )
                .unwrap();
        }
        {
            let journal = AuditJournal::open(&path).unwrap();
            assert_eq!(
                journal
                    .request_outcome(&id, "infrastructure__service_restart", "jupiter", &digest)
                    .unwrap()
                    .as_deref(),
                Some("succeeded")
            );
            assert!(journal
                .record(
                    &id,
                    "infrastructure__service_restart",
                    "jupiter",
                    &digest,
                    "started"
                )
                .is_err());
        }
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn concurrent_requests_cannot_share_a_durable_start_identity() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-concurrent-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let journal = std::sync::Arc::new(AuditJournal::open(&path).unwrap());
        let mut workers = Vec::new();
        for _ in 0..8 {
            let journal = journal.clone();
            workers.push(std::thread::spawn(move || {
                journal
                    .record(
                        &"d".repeat(32),
                        "infrastructure__service_restart",
                        "jupiter",
                        &"e".repeat(64),
                        "started",
                    )
                    .is_ok()
            }));
        }
        assert_eq!(
            workers
                .into_iter()
                .map(|job| job.join().unwrap())
                .filter(|admitted| *admitted)
                .count(),
            1
        );
        assert_eq!(
            journal
                .request_outcome(
                    &"d".repeat(32),
                    "infrastructure__service_restart",
                    "jupiter",
                    &"e".repeat(64)
                )
                .unwrap()
                .as_deref(),
            Some("started")
        );
        drop(journal);
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn complete_outcome_requires_one_matching_start() {
        let id = "b".repeat(32);
        let mut seen = BTreeMap::new();
        let started = event(&id, "started");
        let completed = event(&id, "accepted");
        assert!(started.valid());
        assert!(transition(&seen, &completed).is_err());
        assert!(transition(&seen, &started).is_ok());
        seen.insert(id.clone(), started);
        assert!(transition(&seen, &completed).is_ok());
        seen.insert(id.clone(), completed);
        assert!(transition(&seen, &event(&id, "failed")).is_err());
        assert!(transition(&seen, &event(&id, "started")).is_err());
    }

    #[test]
    fn refuses_substituted_targets_and_argument_digests() {
        let id = "c".repeat(32);
        let mut seen = BTreeMap::new();
        seen.insert(id.clone(), event(&id, "started"));

        let mut wrong_target = event(&id, "succeeded");
        wrong_target.target = "venus".into();
        assert!(transition(&seen, &wrong_target).is_err());

        let mut wrong_args = event(&id, "succeeded");
        wrong_args.arguments_sha256 = "d".repeat(64);
        assert!(transition(&seen, &wrong_args).is_err());
        assert!(transition(&seen, &event(&id, "succeeded")).is_ok());
    }

    #[test]
    fn indeterminate_execution_requires_reconciliation_after_restart() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-audit-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let id = "d".repeat(32);
        {
            let journal = AuditJournal::open(&path).unwrap();
            journal
                .record(
                    &id,
                    "infrastructure__service_restart",
                    "jupiter",
                    &"a".repeat(64),
                    "started",
                )
                .unwrap();
            journal
                .record(
                    &id,
                    "infrastructure__service_restart",
                    "jupiter",
                    &"a".repeat(64),
                    "indeterminate",
                )
                .unwrap();
            let pending = journal.unresolved().unwrap();
            assert_eq!(pending.len(), 1);
            assert_eq!(pending[0].outcome, "indeterminate");
        }
        {
            let journal = AuditJournal::open(&path).unwrap();
            let pending = journal.unresolved().unwrap();
            assert_eq!(pending.len(), 1);
            assert_eq!(pending[0].id, id);
            assert_eq!(pending[0].outcome, "indeterminate");
        }
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn legacy_failed_effects_require_operator_reconciliation() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-legacy-failure-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let first = "e".repeat(32);
        let repeat = "f".repeat(32);
        let digest = "a".repeat(64);
        {
            let journal = AuditJournal::open(&path).unwrap();
            journal
                .record(
                    &first,
                    "infrastructure__service_restart",
                    "saturn",
                    &digest,
                    "started",
                )
                .unwrap();
            journal
                .record(
                    &first,
                    "infrastructure__service_restart",
                    "saturn",
                    &digest,
                    "failed",
                )
                .unwrap();
        }
        {
            let journal = AuditJournal::open(&path).unwrap();
            assert_eq!(journal.unresolved().unwrap()[0].outcome, "failed");
            assert!(matches!(
                journal.record(
                    &repeat,
                    "infrastructure__service_restart",
                    "saturn",
                    &digest,
                    "started"
                ),
                Err(Error::Denied(_))
            ));
            journal.reconcile(&first, &"d".repeat(64)).unwrap();
            journal
                .record(
                    &repeat,
                    "infrastructure__service_restart",
                    "saturn",
                    &digest,
                    "started",
                )
                .unwrap();
        }
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn unresolved_attempt_blocks_matching_resubmission_without_poisoning_audit() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-replay-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let digest = "a".repeat(64);
        {
            let journal = AuditJournal::open(&path).unwrap();
            let first = "1".repeat(32);
            let replay = "2".repeat(32);
            journal
                .record(
                    &first,
                    "infrastructure__service_restart",
                    "jupiter",
                    &digest,
                    "started",
                )
                .unwrap();
            assert!(matches!(
                journal.record(
                    &replay,
                    "infrastructure__service_restart",
                    "jupiter",
                    &digest,
                    "started"
                ),
                Err(Error::Denied(_))
            ));
            journal
                .record(
                    &first,
                    "infrastructure__service_restart",
                    "jupiter",
                    &digest,
                    "indeterminate",
                )
                .unwrap();
            assert!(matches!(
                journal.record(
                    &replay,
                    "infrastructure__service_restart",
                    "jupiter",
                    &digest,
                    "started"
                ),
                Err(Error::Denied(_))
            ));
            // A separate target or canonical argument digest remains eligible.
            journal
                .record(
                    &replay,
                    "infrastructure__service_restart",
                    "jupiter",
                    &"b".repeat(64),
                    "started",
                )
                .unwrap();
        }
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn reconciliation_preserves_evidence_and_ends_the_replay_barrier() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-reconciled-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let id = "a".repeat(32);
        let replay = "b".repeat(32);
        let digest = "c".repeat(64);
        let evidence = "d".repeat(64);
        {
            let journal = AuditJournal::open(&path).unwrap();
            journal
                .record(
                    &id,
                    "infrastructure__service_restart",
                    "saturn",
                    &digest,
                    "started",
                )
                .unwrap();
            journal
                .record(
                    &id,
                    "infrastructure__service_restart",
                    "saturn",
                    &digest,
                    "indeterminate",
                )
                .unwrap();
            assert!(journal.reconcile(&id, "not-a-digest").is_err());
            journal.reconcile(&id, &evidence).unwrap();
            assert!(journal.unresolved().unwrap().is_empty());
            assert!(journal.reconcile(&id, &evidence).is_err());
        }
        {
            let journal = AuditJournal::open(&path).unwrap();
            assert!(journal.unresolved().unwrap().is_empty());
            journal
                .record(
                    &replay,
                    "infrastructure__service_restart",
                    "saturn",
                    &digest,
                    "started",
                )
                .unwrap();
        }
        let raw = fs::read_to_string(&path).unwrap();
        assert!(raw.lines().any(|line| {
            serde_json::from_str::<serde_json::Value>(line)
                .ok()
                .is_some_and(|value| {
                    value
                        .get("reconciliation_sha256")
                        .and_then(serde_json::Value::as_str)
                        == Some(evidence.as_str())
                })
        }));
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn rejects_unjustified_reconciliation_records() {
        let id = "e".repeat(32);
        let prior = event(&id, "started");
        let mut seen = BTreeMap::new();
        seen.insert(id.clone(), prior);
        let mut invalid = event(&id, "reconciled");
        assert!(!invalid.valid());
        invalid.reconciliation_sha256 = Some("f".repeat(64));
        assert!(invalid.valid());
        assert!(transition(&seen, &invalid).is_ok());
        seen.insert(id.clone(), event(&id, "accepted"));
        assert!(transition(&seen, &invalid).is_err());
        invalid.arguments_sha256 = "1".repeat(64);
        assert!(transition(&seen, &invalid).is_err());
    }

    #[test]
    fn invalid_events_fail_closed() {
        let mut event = event(&"a".repeat(32), "started");
        assert!(event.valid());
        event.at = "0001-01-01T00:00:00Z".into();
        assert!(!event.valid());
        event.at = "2026-11-10T18:00:00Z".into();
        event.outcome = "unknown".into();
        assert!(!event.valid());
    }
    #[test]
    fn service_waits_for_release_owner_without_sharing_the_lease() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-wait-{}",
            crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let owner = StateLease::acquire(&directory).unwrap();
        assert!(matches!(
            AuditJournal::open_waiting_for_release(&path, Duration::ZERO),
            Err(Error::Storage("another executor owns the state"))
        ));
        let waiting_path = path.clone();
        let (sender, receiver) = std::sync::mpsc::channel();
        let worker = std::thread::spawn(move || {
            let success =
                AuditJournal::open_waiting_for_release(&waiting_path, Duration::from_secs(2))
                    .is_ok();
            sender.send(success).unwrap();
        });
        assert!(receiver.recv_timeout(Duration::from_millis(50)).is_err());
        drop(owner);
        assert!(receiver.recv_timeout(Duration::from_secs(2)).unwrap());
        worker.join().unwrap();
        fs::remove_dir_all(directory).unwrap();
    }

}
