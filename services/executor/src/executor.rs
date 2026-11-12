//! Canonical admission, independent authorization, audit and restricted execution.
use std::os::unix::process::CommandExt;
use std::process::Stdio;
use std::sync::Arc;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use tokio::process::Command;
use zeroize::Zeroizing;

use crate::audit::AuditJournal;
use crate::authorization::{AuthorizationClient, CredentialTicket, Decision};
use crate::canonical::canonical_arguments;
use crate::policy::{Action, Policy};
use crate::release::{Manifest, ReleaseController};
use crate::workspace;
use crate::{random_hex, valid_hex, Error};

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ExecutionRequest {
    pub request_id: String,
    pub operation: String,
    pub target: String,
    pub arguments: Value,
}

fn is_false(value: &bool) -> bool {
    !*value
}

#[derive(Serialize)]
pub struct ExecutionResult {
    #[serde(flatten)]
    pub decision: Decision,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub state: Option<&'static str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub audit_id: Option<String>,
    #[serde(default, skip_serializing_if = "is_false")]
    pub replayed: bool,
}
impl ExecutionResult {
    pub fn denied(reason: &str) -> Self {
        Self {
            decision: Decision::denied(reason),
            state: None,
            audit_id: None,
            replayed: false,
        }
    }
    pub fn from_decision(decision: Decision) -> Self {
        Self {
            decision,
            state: None,
            audit_id: None,
            replayed: false,
        }
    }
    fn approved(state: &'static str, audit_id: String) -> Self {
        Self {
            decision: Decision {
                allowed: true,
                ..Decision::default()
            },
            state: Some(state),
            audit_id: Some(audit_id),
            replayed: false,
        }
    }
    fn recorded(outcome: &str, id: &str) -> Self {
        match outcome {
            "accepted" | "succeeded" => Self {
                decision: Decision {
                    allowed: true,
                    ..Decision::default()
                },
                state: Some(if outcome == "succeeded" {
                    "completed"
                } else {
                    "accepted"
                }),
                audit_id: Some(id.to_owned()),
                replayed: true,
            },
            "reconciled" => Self::denied(
                "execution request identity was reconciled and cannot be executed again",
            ),
            // A started or indeterminate record may have applied its effect.
            _ => Self::denied("execution request outcome requires independent reconciliation"),
        }
    }
}

#[derive(Serialize)]
pub struct StageResult {
    #[serde(flatten)]
    pub decision: Decision,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub manifest: Option<Manifest>,
}

pub struct Executor {
    pub policy: Arc<Policy>,
    pub authorization: AuthorizationClient,
    pub release: Arc<ReleaseController>,
    pub audit: Arc<AuditJournal>,
}
impl Executor {
    pub fn initialize() -> Result<Arc<Self>, Error> {
        let policy = Arc::new(Policy::load()?);
        let authorization = AuthorizationClient::load()?;
        let audit = Arc::new(AuditJournal::open(
            &policy.state_dir.join("executor-actions.jsonl"),
        )?);
        let release = Arc::new(ReleaseController::new(policy.clone()));
        Ok(Arc::new(Self {
            policy,
            authorization,
            release,
            audit,
        }))
    }

    async fn actuator(&self, argv: &[String]) -> Result<(), Error> {
        let policy = &self.policy;
        let mut command = Command::new(&policy.actuator);
        command
            .args(argv)
            .env_clear()
            .env("HOME", "/var/empty")
            .env("LANG", "C")
            .env("PATH", "/run/current-system/sw/bin:/usr/bin:/bin")
            .env("HOMELAB_ACTUATOR_SYSTEMCTL", &policy.systemctl)
            .env("HOMELAB_ACTUATOR_SERVICES", &policy.service_policy_file)
            .env(
                "HOMELAB_ACTUATOR_KUBERNETES",
                &policy.deployment_policy_file,
            )
            .env(
                "HOMELAB_ACTUATOR_MAX_REPLICAS",
                policy.max_replicas.to_string(),
            )
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .kill_on_drop(true);
        if let (Some(binary), Some(config)) = (&policy.kubectl, &policy.kubeconfig) {
            command
                .env("HOMELAB_ACTUATOR_KUBECTL", binary)
                .env("HOMELAB_ACTUATOR_KUBECONFIG", config);
        }
        command.as_std_mut().process_group(0);
        let mut child = command
            .spawn()
            .map_err(|_| Error::Unavailable("restricted Rust actuator unavailable"))?;
        let pid = child
            .id()
            .ok_or(Error::Unavailable("missing actuator process identity"))?;
        let result = tokio::time::timeout(Duration::from_secs(180), child.wait())
            .await
            .map_err(|_| Error::Unavailable("actuator deadline exceeded"))
            .and_then(|r| r.map_err(|_| Error::Unavailable("actuator wait failed")))
            .and_then(|status| {
                if status.success() {
                    Ok(())
                } else {
                    Err(Error::Denied("restricted actuator refused operation"))
                }
            });
        if result.is_err() {
            unsafe {
                libc::kill(-(pid as i32), libc::SIGKILL);
            }
        }
        result
    }

    async fn perform(
        &self,
        action: Action,
        operation: &str,
        target: &str,
        canonical: &[u8],
        tickets: &[CredentialTicket],
    ) -> Result<&'static str, Error> {
        match action {
            Action::Actuator(args) => {
                self.actuator(&args).await?;
                Ok("accepted")
            }
            Action::Workspace { tool, arguments } => {
                workspace::call(
                    &self.policy,
                    &self.authorization,
                    &tool,
                    arguments,
                    operation,
                    target,
                    canonical,
                    tickets,
                )
                .await?;
                Ok("accepted")
            }
            Action::Release {
                release_id,
                activate,
            } => {
                if activate {
                    self.release.activate(&release_id).await?;
                } else {
                    self.release.revert(&release_id).await?;
                }
                Ok("completed")
            }
        }
    }

    /// The operation task is detached from the HTTP request. A disconnected
    /// caller cannot cancel auditing or silently interrupt a release rollback.
    /// The authorization decision, durable audit, and side effect run in
    /// one task independent of the lifetime of the calling HTTP request.
    pub async fn execute(
        self: &Arc<Self>,
        request: ExecutionRequest,
        assertion: &str,
    ) -> Result<ExecutionResult, Error> {
        if !valid_hex(&request.request_id, 32) {
            return Ok(ExecutionResult::denied(
                "invalid execution request identity",
            ));
        }
        let raw = serde_json::to_vec(&request.arguments)
            .map_err(|_| Error::Denied("invalid operation arguments"))?;
        let canonical = canonical_arguments(&raw)?;
        let value: Value = serde_json::from_slice(&canonical)
            .map_err(|_| Error::Denied("invalid canonical arguments"))?;
        let fields = value
            .as_object()
            .ok_or(Error::Denied("argument object required"))?;
        let action = match self
            .policy
            .admit(&request.operation, &request.target, fields)
        {
            Ok(action) => action,
            Err(error) => return Ok(ExecutionResult::denied(&error.to_string())),
        };
        let executor = self.clone();
        let operation = request.operation;
        let target = request.target;
        let request_id = request.request_id;
        let assertion = Zeroizing::new(assertion.to_owned());
        let job = tokio::spawn(async move {
            let digest = hex::encode(Sha256::digest(&canonical));
            if let Some(outcome) =
                executor
                    .audit
                    .request_outcome(&request_id, &operation, &target, &digest)?
            {
                let replay = executor
                    .authorization
                    .replay(&operation, &target, &canonical, &assertion)
                    .await?;
                if !replay.allowed {
                    return Ok(ExecutionResult::from_decision(replay));
                }
                return Ok(ExecutionResult::recorded(&outcome, &request_id));
            }
            let decision = executor
                .authorization
                .decision(&operation, &target, &canonical, &assertion, true)
                .await?;
            if !decision.allowed {
                return Ok(ExecutionResult::from_decision(decision));
            }
            // The journal append is the atomic execution-admission point.
            // Competing requests with the same ID cannot both pass it.
            if let Err(error) =
                executor
                    .audit
                    .record(&request_id, &operation, &target, &digest, "started")
            {
                return match executor.audit.request_outcome(
                    &request_id,
                    &operation,
                    &target,
                    &digest,
                )? {
                    Some(outcome) => Ok(ExecutionResult::recorded(&outcome, &request_id)),
                    None => Err(error),
                };
            }
            let result = executor
                .perform(
                    action,
                    &operation,
                    &target,
                    &canonical,
                    &decision.credential_tickets,
                )
                .await;
            let outcome = match &result {
                Ok("completed") => "succeeded",
                Ok(_) => "accepted",
                // External effects can occur before an error is observed.
                Err(_) => "indeterminate",
            };
            executor
                .audit
                .record(&request_id, &operation, &target, &digest, outcome)?;
            result.map(|state| ExecutionResult::approved(state, request_id))
        });
        job.await
            .map_err(|_| Error::Unavailable("privileged execution outcome unknown"))?
    }

    pub async fn stage(
        self: &Arc<Self>,
        host: &str,
        revision: &str,
        assertion: &str,
    ) -> Result<StageResult, Error> {
        if !self.policy.release_enabled || host != self.policy.host || !valid_hex(revision, 40) {
            return Err(Error::Denied("release staging target or revision denied"));
        }
        let args = canonical_arguments(
            &serde_json::to_vec(&json!({"host":host,"revision":revision}))
                .map_err(|_| Error::Protocol("stage argument serialization failed"))?,
        )?;
        let owner = self.clone();
        let assertion = Zeroizing::new(assertion.to_owned());
        let revision = revision.to_owned();
        let host = host.to_owned();
        let job = tokio::spawn(async move {
            let operation = "infrastructure__release_stage";
            let decision = owner
                .authorization
                .decision(operation, &host, &args, &assertion, true)
                .await?;
            if !decision.allowed {
                return Ok(StageResult {
                    decision,
                    manifest: None,
                });
            }
            let id = random_hex::<16>()?;
            let digest = hex::encode(Sha256::digest(&args));
            owner
                .audit
                .record(&id, operation, &host, &digest, "started")?;
            let staged = owner.release.stage(&revision).await;
            let outcome = if staged.is_ok() {
                "succeeded"
            } else {
                "indeterminate"
            };
            owner
                .audit
                .record(&id, operation, &host, &digest, outcome)?;
            staged.map(|manifest| StageResult {
                decision: Decision {
                    allowed: true,
                    ..Decision::default()
                },
                manifest: Some(manifest),
            })
        });
        job.await
            .map_err(|_| Error::Unavailable("release staging outcome unknown"))?
    }

    pub async fn preview(
        &self,
        host: &str,
        release_id: &str,
        action: &str,
        assertion: &str,
    ) -> Result<Decision, Error> {
        if !self.policy.release_enabled || host != self.policy.host || !valid_hex(release_id, 48) {
            return Err(Error::Denied("release preflight target denied"));
        }
        let operation = match action {
            "activate" => "infrastructure__release_activate",
            "revert" => "infrastructure__release_revert",
            _ => return Err(Error::Denied("unknown release action")),
        };
        self.release.verify_stage(release_id).await?;
        let args = canonical_arguments(
            &serde_json::to_vec(&json!({"host":host,"release_id":release_id}))
                .map_err(|_| Error::Protocol("preflight argument serialization failed"))?,
        )?;
        self.authorization
            .decision(operation, host, &args, assertion, false)
            .await
    }

    /// A started entry with no terminal record is not assumed to have failed.
    /// Only an independently authorized coordinator can inspect these receipts.
    pub async fn audit_status(&self, assertion: &str) -> Result<Value, Error> {
        let host = &self.policy.host;
        let args = canonical_arguments(
            &serde_json::to_vec(&json!({"host":host}))
                .map_err(|_| Error::Protocol("audit observation encoding failed"))?,
        )?;
        let decision = self
            .authorization
            .decision("infrastructure__audit_status", host, &args, assertion, true)
            .await?;
        if !decision.allowed {
            return Err(Error::Denied("audit observation not authorized"));
        }
        let pending = self.audit.unresolved()?;
        Ok(json!({"host":host,"unresolved":pending,"count":pending.len()}))
    }

    pub async fn observe(&self, assertion: &str) -> Result<Value, Error> {
        if !self.policy.release_enabled {
            return Err(Error::Denied("release observation disabled"));
        }
        let host = &self.policy.host;
        let args = canonical_arguments(
            &serde_json::to_vec(&json!({"host":host}))
                .map_err(|_| Error::Protocol("release observation encoding failed"))?,
        )?;
        let decision = self
            .authorization
            .decision(
                "infrastructure__release_status",
                host,
                &args,
                assertion,
                true,
            )
            .await?;
        if !decision.allowed {
            return Err(Error::Denied("release observation not authorized"));
        }
        let status = self.release.status().await?;
        Ok(json!({"id":status.id,"phase":status.phase}))
    }
}
