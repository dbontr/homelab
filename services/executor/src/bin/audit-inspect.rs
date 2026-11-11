//! Trusted host-console inspection and evidence-bound audit reconciliation.
//! The active Go or Rust executor must first release its state lease.
use std::fs::OpenOptions;
use std::io::Read;
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};

use chrono::{DateTime, Utc};
use homelab_executor_authority::audit::{AuditJournal, Event};
use homelab_executor_authority::policy::absolute;
use homelab_executor_authority::valid_hex;
use serde::Deserialize;
use serde_json::json;
use sha2::{Digest, Sha256};

const MAX_EVIDENCE_BYTES: usize = 8192;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct OperatorEvidence {
    attempt_id: String,
    operation: String,
    target: String,
    arguments_sha256: String,
    finding: String,
    verified_by: String,
    inspected_at: String,
    explanation: String,
}

fn read_evidence(path: &Path) -> Result<Vec<u8>, &'static str> {
    if !path.is_absolute()
        || path.components().any(|component|
            matches!(component, std::path::Component::ParentDir)) {
        return Err("evidence path must be absolute and unambiguous");
    }
    let file = OpenOptions::new().read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
        .open(path).map_err(|_| "operator evidence file unavailable")?;
    let metadata = file.metadata().map_err(|_| "operator evidence metadata unavailable")?;
    if !metadata.is_file() || metadata.uid() != 0 || metadata.mode() & 0o077 != 0 {
        return Err("operator evidence must be a root-owned private regular file");
    }
    let mut bytes = Vec::new();
    file.take((MAX_EVIDENCE_BYTES+1) as u64)
        .read_to_end(&mut bytes).map_err(|_| "operator evidence unreadable")?;
    if bytes.is_empty() || bytes.len()>MAX_EVIDENCE_BYTES {
        return Err("operator evidence exceeds permitted size");
    }
    Ok(bytes)
}

fn validate_evidence(raw: &[u8], attempt: &Event) -> Result<String, &'static str> {
    let note: OperatorEvidence =
        serde_json::from_slice(raw).map_err(|_| "invalid operator evidence record")?;
    if note.attempt_id != attempt.id || note.operation != attempt.operation
        || note.target != attempt.target
        || note.arguments_sha256 != attempt.arguments_sha256
        || !matches!(attempt.outcome.as_str(), "started"|"indeterminate") {
        return Err("operator evidence does not match unresolved attempt");
    }
    if !matches!(note.finding.as_str(), "applied"|"not_applied")
        || note.verified_by.trim().len()<2 || note.verified_by.len()>128
        || note.explanation.trim().len()<32 || note.explanation.len()>2000 {
        return Err("operator evidence requires an exact finding and verification explanation");
    }
    let inspected = DateTime::parse_from_rfc3339(&note.inspected_at)
        .map_err(|_| "operator inspection time is invalid")?;
    let started = DateTime::parse_from_rfc3339(&attempt.at)
        .map_err(|_| "original audit time is invalid")?;
    if inspected < started || inspected.timestamp() >
        Utc::now().timestamp().saturating_add(300) {
        return Err("operator inspection time does not follow the attempted operation");
    }
    Ok(hex::encode(Sha256::digest(raw)))
}

fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if unsafe { libc::geteuid() } != 0 {
        return Err("audit recovery requires a trusted local root terminal".into());
    }
    let command: Vec<String> = std::env::args().skip(1).collect();
    if command.len()!=1 && (command.len()!=4
        || command[0]!="reconcile" || command[3]!="--confirm") {
        return Err("usage: audit-inspect unresolved | reconcile ID EVIDENCE_FILE --confirm".into());
    }
    let state: PathBuf = absolute("HOMELAB_EXECUTOR_RELEASE_STATE")?;
    let audit = AuditJournal::open(&state.join("executor-actions.jsonl"))?;
    if command.len()==1 {
        if command[0]!="unresolved" {
            return Err("usage: audit-inspect unresolved".into());
        }
        let incomplete = audit.unresolved()?;
        println!("{}", serde_json::to_string(&json!({
            "unresolved": incomplete,
            "count": incomplete.len(),
        }))?);
        return Ok(());
    }
    let id = &command[1];
    if !valid_hex(id,32) {
        return Err("an exact 32-hex audit identity is required".into());
    }
    let attempts = audit.unresolved()?;
    let attempt = attempts.iter().find(|event|&event.id==id)
        .ok_or("audit identity is not an unresolved attempt")?;
    let evidence = read_evidence(Path::new(&command[2]))?;
    let digest = validate_evidence(&evidence,attempt)?;
    audit.reconcile(id,&digest)?;
    println!("operator reconciliation recorded for audit identity {id}");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn event() -> Event {
        Event {
            id:"a".repeat(32),
            operation:"infrastructure__service_restart".into(),
            target:"saturn".into(),
            arguments_sha256:"b".repeat(64),
            outcome:"indeterminate".into(),
            at:"2026-11-10T18:00:00Z".into(),
            reconciliation_sha256:None,
        }
    }

    #[test]
    fn reconciliation_requires_exact_identity_and_independent_inspection() {
        let attempt=event();
        let note=json!({
            "attempt_id":attempt.id,"operation":attempt.operation,
            "target":attempt.target,"arguments_sha256":attempt.arguments_sha256,
            "finding":"applied","verified_by":"local administrator",
            "inspected_at":"2026-11-10T18:30:00Z",
            "explanation":"Confirmed that the designated service received the intended restart using its independent journal."
        });
        let raw=note.to_string();
        assert_eq!(validate_evidence(raw.as_bytes(),&attempt).unwrap().len(),64);
        let mut wrong=note.clone();
        wrong["target"]=json!("venus");
        assert!(validate_evidence(wrong.to_string().as_bytes(),&attempt).is_err());
        wrong=note;
        wrong["finding"]=json!("unknown");
        assert!(validate_evidence(wrong.to_string().as_bytes(),&attempt).is_err());
    }
}
