//! Non-executing acceptance tests for privileged policy and durable audit state.
use std::collections::BTreeSet;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use homelab_executor_authority::audit::AuditJournal;
use homelab_executor_authority::policy::{Action, Policy};
use serde_json::json;

struct PrivateJournal(PathBuf);
impl PrivateJournal {
    fn new() -> Self {
        let path = std::env::temp_dir().join(format!(
            "executor-acceptance-{}",
            homelab_executor_authority::random_hex::<16>().unwrap()
        ));
        fs::create_dir(&path).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        Self(path)
    }
    fn path(&self) -> PathBuf {
        self.0.join("actions.jsonl")
    }
}
impl Drop for PrivateJournal {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn policy() -> Policy {
    let inert = PathBuf::from("/nonexistent-test-only");
    Policy {
        host: "saturn".into(),
        release_enabled: false,
        systemctl: inert.clone(),
        actuator: inert.clone(),
        service_policy_file: inert.clone(),
        deployment_policy_file: inert.clone(),
        services: BTreeSet::from(["research-runner.service".into()]),
        deployments: BTreeSet::from(["pwm-system/research-worker".into()]),
        max_replicas: 3,
        workspace: None,
        workspace_token: None,
        workspace_vault_alias: None,
        state_dir: inert.clone(),
        source: inert.clone(),
        nix: inert.clone(),
        nix_env: inert.clone(),
        git: inert.clone(),
        health: vec!["research-runner.service".into()],
        health_sockets: Vec::new(),
        kubeconfig: Some(inert.clone()),
        kubectl: Some(inert),
    }
}

#[test]
fn executor_admits_only_matching_host_resources_arguments_and_enabled_operation() {
    let mut policy = policy();
    let approved = json!({"host":"saturn", "service":"research-runner.service"});
    assert!(matches!(
        policy.admit(
            "infrastructure__service_restart",
            "saturn",
            approved.as_object().unwrap()
        ),
        Ok(Action::Actuator(_))
    ));
    assert!(policy
        .admit(
            "infrastructure__service_restart",
            "jupiter",
            approved.as_object().unwrap()
        )
        .is_err());
    assert!(policy
        .admit(
            "infrastructure__service_restart",
            "saturn",
            json!({"host":"jupiter", "service":"research-runner.service"})
                .as_object()
                .unwrap()
        )
        .is_err());
    assert!(policy
        .admit(
            "infrastructure__service_restart",
            "saturn",
            json!({"host":"saturn", "service":"homelab-securityd.service"})
                .as_object()
                .unwrap()
        )
        .is_err());
    assert!(policy
        .admit(
            "infrastructure__service_restart",
            "saturn",
            json!({"host":"saturn", "service":"research-runner.service", "unexpected":true})
                .as_object()
                .unwrap()
        )
        .is_err());
    assert!(policy
        .admit(
            "infrastructure__unregistered",
            "saturn",
            approved.as_object().unwrap()
        )
        .is_err());
    let kube = json!({"host":"saturn", "namespace":"pwm-system", "deployment":"research-worker"});
    assert!(matches!(
        policy.admit(
            "cluster__deployment_restart",
            "saturn",
            kube.as_object().unwrap()
        ),
        Ok(Action::Actuator(_))
    ));
    assert!(policy
        .admit(
            "cluster__deployment_restart",
            "saturn",
            json!({"host":"saturn", "namespace":"kube-system", "deployment":"research-worker"})
                .as_object()
                .unwrap()
        )
        .is_err());
    assert!(policy.admit("cluster__deployment_scale", "saturn", json!({"host":"saturn", "namespace":"pwm-system", "deployment":"research-worker", "replicas":4}).as_object().unwrap()).is_err());
    assert!(policy.admit("cluster__deployment_scale", "saturn", json!({"host":"saturn", "namespace":"pwm-system", "deployment":"research-worker", "replicas":2, "resource_version":"bad/version"}).as_object().unwrap()).is_err());
    let release = json!({"host":"saturn", "release_id":"a".repeat(48)});
    assert!(policy
        .admit(
            "infrastructure__release_activate",
            "saturn",
            release.as_object().unwrap()
        )
        .is_err());
    policy.release_enabled = true;
    assert!(matches!(
        policy.admit(
            "infrastructure__release_activate",
            "saturn",
            release.as_object().unwrap()
        ),
        Ok(Action::Release { .. })
    ));
    // Admission is an offline policy decision; it never invokes an actuator.
}

#[test]
fn restart_replay_and_operator_reconciliation_preserve_exact_durable_identity() {
    let scratch = PrivateJournal::new();
    let path = scratch.path();
    let original = "a".repeat(32);
    let next = "b".repeat(32);
    let digest = "c".repeat(64);
    let operation = "infrastructure__service_restart";
    {
        let journal = AuditJournal::open(&path).unwrap();
        assert!(AuditJournal::open(&path).is_err());
        journal
            .record(&original, operation, "saturn", &digest, "started")
            .unwrap();
        assert_eq!(journal.unresolved().unwrap().len(), 1);
        assert!(journal
            .record(&next, operation, "saturn", &digest, "started")
            .is_err());
        assert!(journal
            .request_outcome(&original, operation, "venus", &digest)
            .is_err());
        assert!(journal
            .request_outcome(&original, operation, "saturn", &"d".repeat(64))
            .is_err());
    }
    {
        let journal = AuditJournal::open(&path).unwrap();
        assert_eq!(
            journal
                .request_outcome(&original, operation, "saturn", &digest)
                .unwrap()
                .as_deref(),
            Some("started")
        );
        assert!(journal
            .record(&next, operation, "saturn", &digest, "started")
            .is_err());
        journal.reconcile(&original, &"e".repeat(64)).unwrap();
        assert!(journal.reconcile(&original, &"f".repeat(64)).is_err());
        journal
            .record(&next, operation, "saturn", &digest, "started")
            .unwrap();
        journal
            .record(&next, operation, "saturn", &digest, "succeeded")
            .unwrap();
        assert!(journal.unresolved().unwrap().is_empty());
    }
    let reopened = AuditJournal::open(&path).unwrap();
    assert_eq!(
        reopened
            .request_outcome(&original, operation, "saturn", &digest)
            .unwrap()
            .as_deref(),
        Some("reconciled")
    );
    assert_eq!(
        reopened
            .request_outcome(&next, operation, "saturn", &digest)
            .unwrap()
            .as_deref(),
        Some("succeeded")
    );
}

#[test]
fn concurrent_duplicate_attempts_admit_exactly_one_durable_start() {
    let scratch = PrivateJournal::new();
    let journal = Arc::new(AuditJournal::open(&scratch.path()).unwrap());
    let admitted = Arc::new(AtomicUsize::new(0));
    std::thread::scope(|tasks| {
        for _ in 0..32 {
            let journal = Arc::clone(&journal);
            let admitted = Arc::clone(&admitted);
            tasks.spawn(move || {
                if journal
                    .record(
                        &"a".repeat(32),
                        "infrastructure__service_restart",
                        "saturn",
                        &"b".repeat(64),
                        "started",
                    )
                    .is_ok()
                {
                    admitted.fetch_add(1, Ordering::SeqCst);
                }
            });
        }
    });
    assert_eq!(admitted.load(Ordering::SeqCst), 1);
    assert_eq!(journal.unresolved().unwrap().len(), 1);
    drop(journal);
    assert_eq!(
        AuditJournal::open(&scratch.path())
            .unwrap()
            .unresolved()
            .unwrap()
            .len(),
        1
    );
}

#[test]
fn corrupted_journal_and_replaced_inode_deny_all_execution_admission() {
    let scratch = PrivateJournal::new();
    let path = scratch.path();
    let journal = AuditJournal::open(&path).unwrap();
    let id = "a".repeat(32);
    let digest = "b".repeat(64);
    journal
        .record(
            &id,
            "infrastructure__service_restart",
            "saturn",
            &digest,
            "started",
        )
        .unwrap();
    let replacement = scratch.0.join("replacement.jsonl");
    fs::write(&replacement, b"").unwrap();
    fs::set_permissions(&replacement, fs::Permissions::from_mode(0o600)).unwrap();
    fs::rename(&replacement, &path).unwrap();
    assert!(journal.unresolved().is_err());
    assert!(journal
        .record(
            &"c".repeat(32),
            "infrastructure__service_restart",
            "saturn",
            &digest,
            "started"
        )
        .is_err());
    drop(journal);
    fs::write(&path, b"{\"partial\":true}").unwrap();
    assert!(AuditJournal::open(&path).is_err());
}

#[test]
fn executor_uses_unambiguous_canonical_argument_identity() {
    use homelab_executor_authority::canonical::canonical_arguments;
    let ordered = br#"{"host":"saturn","release_id":"0123456789abcdef"}"#;
    let reordered = br#"{"release_id":"0123456789abcdef","host":"saturn"}"#;
    assert_eq!(
        canonical_arguments(ordered).unwrap(),
        canonical_arguments(reordered).unwrap()
    );
    for ambiguous in [
        br#"{"host":"saturn","host":"jupiter"}"#.as_slice(),
        br#"[]"#.as_slice(),
        br#"{"host":"saturn"}junk"#.as_slice(),
    ] {
        assert!(canonical_arguments(ambiguous).is_err());
    }
}
