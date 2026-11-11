//! Durable, inode-fenced privileged execution journal with exclusive state custody.
use std::collections::BTreeMap;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

use chrono::{DateTime, Datelike, Utc};
use fs2::FileExt;
use serde::{Deserialize, Serialize};

use crate::{valid_hex, valid_host, Error};

const MAX_AUDIT_BYTES: u64 = 64 << 20;

fn owner_only(metadata: &fs::Metadata) -> bool {
    metadata.is_file() && metadata.mode() & 0o077 == 0
        && metadata.uid() == unsafe { libc::geteuid() }
}

fn private_directory(directory: &Path) -> Result<(), Error> {
    if !directory.is_absolute() || directory == Path::new("/")
        || directory.components().any(|p| matches!(p,std::path::Component::ParentDir))
    { return Err(Error::Configuration("private executor state directory required")); }
    let metadata = fs::symlink_metadata(directory)
        .map_err(|_| Error::Storage("executor state directory unavailable"))?;
    if !metadata.is_dir() || metadata.file_type().is_symlink() || metadata.mode() & 0o077 != 0
        || metadata.uid() != unsafe { libc::geteuid() } {
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
        let file = OpenOptions::new().read(true).write(true).create(true).mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(directory.join(".executord.lock"))
            .map_err(|_| Error::Storage("executor state lock unavailable"))?;
        if !owner_only(&file.metadata().map_err(|_| Error::Storage("lock metadata unavailable"))?) {
            return Err(Error::Storage("unsafe executor state lock"));
        }
        file.try_lock_exclusive().map_err(|_| Error::Storage("another executor owns the state"))?;
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
        valid_hex(&self.id, 32) && valid_hex(&self.arguments_sha256, 64)
            && !self.operation.is_empty() && self.operation.len() <= 128
            && valid_host(&self.target)
            && matches!(self.outcome.as_str(), "started" | "accepted" | "succeeded" | "failed"
                | "indeterminate" | "reconciled")
            && if self.outcome == "reconciled" {
                self.reconciliation_sha256.as_deref().is_some_and(|digest| valid_hex(digest, 64))
            } else {
                self.reconciliation_sha256.is_none()
            }
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
        if prior.is_some() { return Err(Error::Storage("duplicate audit identity")); }
        return Ok(());
    }
    let prior = prior.ok_or(Error::Storage("audit outcome has no start event"))?;
    let before = DateTime::parse_from_rfc3339(&prior.at)
        .map_err(|_| Error::Storage("invalid audit time"))?;
    let after = DateTime::parse_from_rfc3339(&current.at)
        .map_err(|_| Error::Storage("invalid audit time"))?;
    let valid_prior = if current.outcome == "reconciled" {
        matches!(prior.outcome.as_str(), "started" | "indeterminate")
    } else {
        prior.outcome == "started"
    };
    if !valid_prior || prior.operation != current.operation
        || prior.target != current.target || prior.arguments_sha256 != current.arguments_sha256
        || after < before {
        return Err(Error::Storage("contradictory audit transition"));
    }
    Ok(())
}

impl AuditJournal {
    pub fn open(path: &Path) -> Result<Self, Error> {
        let parent = path.parent().ok_or(Error::Configuration("audit directory required"))?;
        let lease = StateLease::acquire(parent)?;
        let mut file = OpenOptions::new().read(true).append(true).create(true).mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC).open(path)
            .map_err(|_| Error::Storage("audit journal unavailable"))?;
        let metadata = file.metadata().map_err(|_| Error::Storage("audit metadata unavailable"))?;
        if !owner_only(&metadata) || metadata.len() > MAX_AUDIT_BYTES {
            return Err(Error::Storage("audit journal custody or size is unsafe"));
        }
        File::open(parent).and_then(|f| f.sync_all())
            .map_err(|_| Error::Storage("audit inode cannot be synchronized"))?;
        file.seek(SeekFrom::Start(0)).map_err(|_| Error::Storage("audit seek failed"))?;
        let mut contents = Vec::new();
        file.by_ref().take(MAX_AUDIT_BYTES + 1).read_to_end(&mut contents)
            .map_err(|_| Error::Storage("audit read failed"))?;
        if contents.len() as u64 > MAX_AUDIT_BYTES ||
            (!contents.is_empty() && contents.last() != Some(&b'\n')) {
            return Err(Error::Storage("audit journal has a partial record"));
        }
        let mut seen = BTreeMap::new();
        let record_data = if contents.is_empty() { &contents[..] }
            else { &contents[..contents.len() - 1] };
        for line in record_data.split(|b| *b == b'\n') {
            if contents.is_empty() { break; }
            if line.is_empty() { return Err(Error::Storage("audit contains an empty record")); }
            if line.len() > 2048 { return Err(Error::Storage("audit record exceeds limit")); }
            let entry: Event = serde_json::from_slice(line)
                .map_err(|_| Error::Storage("invalid audit record"))?;
            if !entry.valid() { return Err(Error::Storage("invalid audit record fields")); }
            transition(&seen, &entry)?;
            seen.insert(entry.id.clone(), entry);
        }
        Ok(Self { path: path.into(), dev: metadata.dev(), ino: metadata.ino(),
            inner: Mutex::new(Journal { file, seen, faulted: false }), _lease: lease })
    }

    /// An uncertain append or directory replacement permanently disables execution.
    /// Return operations with no confirmed effect. Both unfinished and
    /// indeterminate attempts require explicit reconciliation before retry.
    pub fn unresolved(&self) -> Result<Vec<Event>, Error> {
        let journal = self.inner.lock().unwrap_or_else(|_| std::process::abort());
        if journal.faulted {
            return Err(Error::Storage("audit journal is in an uncertain state"));
        }
        let path_meta = fs::symlink_metadata(&self.path)
            .map_err(|_| Error::Storage("audit path missing"))?;
        let fd_meta = journal.file.metadata()
            .map_err(|_| Error::Storage("audit descriptor unavailable"))?;
        if !owner_only(&fd_meta) || path_meta.dev() != self.dev ||
            path_meta.ino() != self.ino || fd_meta.dev() != self.dev || fd_meta.ino() != self.ino {
            return Err(Error::Storage("audit journal was replaced"));
        }
        Ok(journal.seen.values().filter(|event|
            matches!(event.outcome.as_str(), "started" | "indeterminate"))
            .cloned().collect())
    }

    pub fn record(&self, id: &str, operation: &str, target: &str, digest: &str,
        outcome: &str) -> Result<(), Error>
    {
        let entry = Event { id: id.into(), operation: operation.into(), target: target.into(),
            arguments_sha256: digest.into(), outcome: outcome.into(),
            at: Utc::now().to_rfc3339(), reconciliation_sha256: None };
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
            if journal.faulted { return Err(Error::Storage("audit journal is in an uncertain state")); }
            journal.seen.get(id).cloned().ok_or(Error::Denied("audit attempt not found"))?
        };
        if !matches!(original.outcome.as_str(), "started" | "indeterminate") {
            return Err(Error::Denied("audit attempt is already resolved"));
        }
        self.append_event(Event {
            id: original.id, operation: original.operation, target: original.target,
            arguments_sha256: original.arguments_sha256, outcome: "reconciled".into(),
            at: Utc::now().to_rfc3339(),
            reconciliation_sha256: Some(evidence_sha256.into()),
        })
    }

    fn append_event(&self, entry: Event) -> Result<(), Error> {
        if !entry.valid() { return Err(Error::Denied("invalid audit event")); }
        let raw = serde_json::to_vec(&entry).map_err(|_| Error::Storage("audit encoding failed"))?;
        if raw.len() > 1024 { return Err(Error::Storage("audit record too large")); }
        let mut journal = self.inner.lock().unwrap_or_else(|_| std::process::abort());
        if journal.faulted { return Err(Error::Storage("audit journal is in an uncertain state")); }
        // Policy refusals are not journal I/O failures. A denied replay
        // leaves the journal usable for unrelated authorized operations.
        transition(&journal.seen, &entry)?;
        if entry.outcome == "started" && journal.seen.values().any(|prior| {
            prior.operation == entry.operation
                && prior.target == entry.target
                && prior.arguments_sha256 == entry.arguments_sha256
                && matches!(prior.outcome.as_str(), "started" | "indeterminate")
        }) {
            return Err(Error::Denied("matching unresolved execution requires reconciliation"));
        }
        let result = (|| {
            let path_meta = fs::symlink_metadata(&self.path)
                .map_err(|_| Error::Storage("audit path missing"))?;
            let fd_meta = journal.file.metadata()
                .map_err(|_| Error::Storage("audit descriptor unavailable"))?;
            if !owner_only(&fd_meta) || path_meta.dev() != self.dev || path_meta.ino() != self.ino
                || fd_meta.dev() != self.dev || fd_meta.ino() != self.ino {
                return Err(Error::Storage("audit journal was replaced"));
            }
            if fd_meta.len() + raw.len() as u64 + 1 > MAX_AUDIT_BYTES {
                return Err(Error::Storage("audit journal reached retention limit"));
            }
            journal.file.write_all(&raw).and_then(|_| journal.file.write_all(b"\n"))
                .and_then(|_| journal.file.sync_all())
                .map_err(|_| Error::Storage("durable audit append failed"))
        })();
        match result {
            Ok(()) => { journal.seen.insert(entry.id.clone(), entry); Ok(()) }
            Err(e) => { journal.faulted = true; Err(e) }
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
            "homelab-executor-audit-{}", crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let id = "d".repeat(32);
        {
            let journal = AuditJournal::open(&path).unwrap();
            journal.record(&id, "infrastructure__service_restart", "jupiter",
                &"a".repeat(64), "started").unwrap();
            journal.record(&id, "infrastructure__service_restart", "jupiter",
                &"a".repeat(64), "indeterminate").unwrap();
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
    fn unresolved_attempt_blocks_matching_resubmission_without_poisoning_audit() {
        let directory = std::env::temp_dir().join(format!(
            "homelab-executor-replay-{}", crate::random_hex::<12>().unwrap()
        ));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let path = directory.join("executor-actions.jsonl");
        let digest = "a".repeat(64);
        {
            let journal = AuditJournal::open(&path).unwrap();
            let first = "1".repeat(32);
            let replay = "2".repeat(32);
            journal.record(&first,"infrastructure__service_restart","jupiter",
                &digest,"started").unwrap();
            assert!(matches!(
                journal.record(&replay,"infrastructure__service_restart","jupiter",
                    &digest,"started"),
                Err(Error::Denied(_))
            ));
            journal.record(&first,"infrastructure__service_restart","jupiter",
                &digest,"indeterminate").unwrap();
            assert!(matches!(
                journal.record(&replay,"infrastructure__service_restart","jupiter",
                    &digest,"started"),
                Err(Error::Denied(_))
            ));
            // A separate target or canonical argument digest remains eligible.
            journal.record(&replay,"infrastructure__service_restart","jupiter",
                &"b".repeat(64),"started").unwrap();
        }
        fs::remove_dir_all(directory).unwrap();
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
}
