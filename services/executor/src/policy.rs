//! Trusted Nix-defined operation and resource policy.
use std::collections::BTreeSet;
use std::env;
use std::fs::OpenOptions;
use std::io::Read;
use std::os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Component, Path, PathBuf};

use serde::Deserialize;
use serde_json::{Map, Value};
use url::Url;

use crate::{valid_hex, valid_host, Error};

const FORBIDDEN_SERVICES: &[&str] = &[
    "network", "firewall", "router", "wireguard", "vpn", "dns", "nftables",
    "iptables", "nginx", "cloudflared", "pihole", "tailscale", "systemd",
    "docker", "k3s", "kube", "jellyfin", "moonfin", "ssh", "security",
    "executor", "fleet", "prometheus", "loki", "alertmanager",
];

pub fn required(name: &str) -> Result<String, Error> {
    env::var(name).ok().filter(|x| !x.is_empty())
        .ok_or(Error::Configuration("required trusted executor configuration missing"))
}

pub fn absolute(name: &str) -> Result<PathBuf, Error> {
    let path = PathBuf::from(required(name)?);
    if !path.is_absolute() || path.components().any(|c|
        matches!(c, Component::ParentDir | Component::CurDir)
    ) { return Err(Error::Configuration("executor path must be canonical and absolute")); }
    Ok(path)
}

pub fn bounded_file(path: &Path, max_bytes: usize, private: bool) -> Result<Vec<u8>, Error> {
    if !path.is_absolute() || path.components().any(|c| matches!(c, Component::ParentDir)) {
        return Err(Error::Configuration("protected file path is not absolute"));
    }
    let mut file = OpenOptions::new().read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC).open(path)
        .map_err(|_| Error::Storage("configured file unavailable"))?;
    let meta = file.metadata().map_err(|_| Error::Storage("file metadata unavailable"))?;
    let mask = if private { 0o077 } else { 0o022 };
    if !meta.is_file() || meta.permissions().mode() & mask != 0
        || (private && meta.uid() != unsafe { libc::geteuid() }) {
        return Err(Error::Storage("configured file has unsafe custody"));
    }
    let mut bytes = Vec::new();
    file.by_ref().take((max_bytes + 1) as u64).read_to_end(&mut bytes)
        .map_err(|_| Error::Storage("cannot read configured file"))?;
    if bytes.len() > max_bytes { return Err(Error::Storage("configured file exceeds limit")); }
    Ok(bytes)
}

fn allowlist_file(path: &Path) -> Result<BTreeSet<String>, Error> {
    let value = bounded_file(path, 64 << 10, false)?;
    let text = std::str::from_utf8(&value).map_err(|_| Error::Configuration("invalid allowlist"))?;
    let mut result = BTreeSet::new();
    for item in text.lines().filter(|s| !s.is_empty()) {
        if item != item.trim() || !result.insert(item.to_owned()) || result.len() > 8192 {
            return Err(Error::Configuration("invalid or duplicated allowlist entry"));
        }
    }
    Ok(result)
}

pub fn valid_service(value: &str) -> bool {
    let lower = value.to_ascii_lowercase();
    value.len() <= 128 && value.len() > ".service".len()
        && value.ends_with(".service")
        && value.bytes().all(|c| c.is_ascii_alphanumeric() || b"_.@-".contains(&c))
        && !FORBIDDEN_SERVICES.iter().any(|name| lower.contains(name))
}

pub fn valid_health_service(value: &str) -> bool {
    const CRITICAL: &[&str] = &[
        "network","firewall","cloudflared","pihole","k3s","tailscale",
        "docker","jellyfin","nginx","systemd-networkd",
    ];
    let lower = value.to_ascii_lowercase();
    !value.is_empty() && value.len() <= 128
        && value.as_bytes()[0].is_ascii_alphanumeric()
        && value.bytes().all(|c| c.is_ascii_alphanumeric() || b"@_.-".contains(&c))
        && !CRITICAL.iter().any(|term| lower.contains(term))
}

pub fn valid_kube_name(value: &str) -> bool {
    value.len() <= 63 && !value.is_empty()
        && (value.as_bytes()[0].is_ascii_lowercase() || value.as_bytes()[0].is_ascii_digit())
        && (value.as_bytes()[value.len()-1].is_ascii_lowercase()
            || value.as_bytes()[value.len()-1].is_ascii_digit())
        && value.bytes().all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == b'-')
}

fn valid_workspace_name(value: &str, package: bool) -> bool {
    let limit = if package { 80 } else { 96 };
    !value.is_empty() && value.len() <= limit
        && value.as_bytes()[0].is_ascii_alphanumeric()
        && value.bytes().all(|c| c.is_ascii_alphanumeric()
            || matches!(c, b'_' | b'.' | b'-')
            || (package && c == b'+'))
}

#[derive(Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct WorkspacePolicy {
    #[serde(rename = "Endpoint")]
    pub endpoint: String,
    #[serde(rename = "Projects")]
    pub projects: Vec<String>,
    #[serde(rename = "Workers")]
    pub workers: Vec<String>,
    #[serde(rename = "Images")]
    pub images: Vec<String>,
    #[serde(rename = "Software")]
    pub software: Vec<String>,
    #[serde(rename = "Packages")]
    pub packages: Vec<String>,
}

impl WorkspacePolicy {
    fn validate(&self) -> Result<(), Error> {
        let url = Url::parse(&self.endpoint).map_err(|_| Error::Configuration("invalid Workspace endpoint"))?;
        let loopback = url.host_str().is_some_and(|h|
            h == "127.0.0.1" || h == "::1" || h == "localhost");
        if url.path() != "/mcp" || url.query().is_some() || url.fragment().is_some()
            || !url.username().is_empty() || url.password().is_some()
            || !(url.scheme() == "https" || (url.scheme() == "http" && loopback)) {
            return Err(Error::Configuration("Workspace must use an exact private MCP endpoint"));
        }
        let all = [&self.projects, &self.workers, &self.images, &self.software, &self.packages];
        if all.iter().all(|names| names.is_empty()) {
            return Err(Error::Configuration("Workspace resource policy is empty"));
        }
        for (i, names) in all.into_iter().enumerate() {
            if names.len() > 64 { return Err(Error::Configuration("Workspace resource policy too large")); }
            let mut seen = BTreeSet::new();
            for name in names {
                if !valid_workspace_name(name, i == 4) || !seen.insert(name) {
                    return Err(Error::Configuration("Workspace resource policy has invalid identifiers"));
                }
            }
        }
        Ok(())
    }

    pub fn operation(&self, name: &str, fields: &Map<String, Value>) -> Result<(String, Value), Error> {
        let string = |key: &str| -> Result<&str, Error> {
            let value = fields.get(key).and_then(Value::as_str)
                .ok_or(Error::Denied("Workspace field is required"))?;
            if !valid_workspace_name(value, false) {
                return Err(Error::Denied("Workspace field is invalid"));
            }
            Ok(value)
        };
        let value = match name {
            "infrastructure__workspace_stop" if fields.len() == 3 => {
                let project = string("project_id")?;
                if !self.projects.iter().any(|s| s == project) { return Err(Error::Denied("project not permitted")); }
                ("workspace_destroy", serde_json::json!({"project_id":project,"workspace_id":string("workspace_id")?}))
            }
            "infrastructure__workspace_build_environment" if fields.len() == 4 => {
                let project = string("project_id")?;
                if !self.projects.iter().any(|s| s == project) { return Err(Error::Denied("project not permitted")); }
                ("environment_build",serde_json::json!({"project_id":project,"workspace_id":string("workspace_id")?,"name":string("name")?}))
            }
            "infrastructure__workspace_revoke_worker" if fields.len() == 2 => {
                let worker = string("node_id")?;
                if !self.workers.iter().any(|s| s == worker) { return Err(Error::Denied("worker not permitted")); }
                ("worker_revoke",serde_json::json!({"node_id":worker}))
            }
            "infrastructure__workspace_register_software" if (fields.len() == 4 || fields.len() == 5) => {
                let image = string("image_id")?;
                let software = string("software_id")?;
                if !self.images.iter().any(|s| s == image) || !self.software.iter().any(|s| s == software) {
                    return Err(Error::Denied("software resource not permitted"));
                }
                let packages = fields.get("packages").and_then(Value::as_array)
                    .ok_or(Error::Denied("package list required"))?;
                if packages.is_empty() || packages.len() > 64 { return Err(Error::Denied("invalid package list")); }
                let mut accepted = BTreeSet::new();
                let mut ordered = Vec::new();
                for package in packages {
                    let name = package.as_str().ok_or(Error::Denied("invalid package identifier"))?;
                    if !valid_workspace_name(name, true) || !self.packages.iter().any(|s| s == name)
                        || !accepted.insert(name) {
                        return Err(Error::Denied("unapproved package"));
                    }
                    ordered.push(name);
                }
                if fields.len() == 5 && !fields.contains_key("description") {
                    return Err(Error::Denied("unexpected Workspace software field"));
                }
                let mut args = serde_json::json!({"image_id":image,"software_id":software,"packages":ordered});
                if let Some(description) = fields.get("description") {
                    let description = description.as_str().ok_or(Error::Denied("invalid description"))?;
                    if description.len() > 512 || description.bytes().any(|b| b == 0 || b == b'\r') {
                        return Err(Error::Denied("invalid description"));
                    }
                    args["description"] = Value::String(description.to_owned());
                }
                ("pool_register_software",args)
            }
            _ => return Err(Error::Denied("unregistered Workspace action or unexpected fields")),
        };
        Ok((value.0.into(), value.1))
    }
}

#[derive(Clone)]
pub struct Policy {
    pub host: String,
    pub release_enabled: bool,
    pub systemctl: PathBuf,
    pub actuator: PathBuf,
    pub service_policy_file: PathBuf,
    pub deployment_policy_file: PathBuf,
    pub services: BTreeSet<String>,
    pub deployments: BTreeSet<String>,
    pub max_replicas: u32,
    pub workspace: Option<WorkspacePolicy>,
    pub workspace_token: Option<PathBuf>,
    pub workspace_vault_alias: Option<String>,
    pub state_dir: PathBuf,
    pub source: PathBuf,
    pub nix: PathBuf,
    pub nix_env: PathBuf,
    pub git: PathBuf,
    pub health: Vec<String>,
    pub kubeconfig: Option<PathBuf>,
    pub kubectl: Option<PathBuf>,
}

#[derive(Clone)]
pub enum Action {
    Actuator(Vec<String>),
    Workspace { tool: String, arguments: Value },
    Release { release_id: String, activate: bool },
}

impl Policy {
    pub fn load() -> Result<Self, Error> {
        let host = required("HOMELAB_EXECUTOR_HOST")?;
        if !valid_host(&host) { return Err(Error::Configuration("invalid host")); }
        let service_policy_file = absolute("HOMELAB_EXECUTOR_RUST_SERVICES")?;
        let services = allowlist_file(&service_policy_file)?;
        if services.len() > 64 || services.iter().any(|s| !valid_service(s)) {
            return Err(Error::Configuration("unsafe systemd allowlist"));
        }
        let deployment_policy_file = absolute("HOMELAB_EXECUTOR_RUST_KUBERNETES")?;
        let deployments = allowlist_file(&deployment_policy_file)?;
        for d in &deployments {
            let (namespace, deployment) = d.split_once('/')
                .ok_or(Error::Configuration("invalid Kubernetes policy"))?;
            if !valid_kube_name(namespace) || !valid_kube_name(deployment) {
                return Err(Error::Configuration("invalid Kubernetes resource name"));
            }
        }
        let max_replicas = required("HOMELAB_EXECUTOR_RUST_MAX_REPLICAS")?
            .parse::<u32>().map_err(|_| Error::Configuration("invalid replica limit"))?;
        if !(1..=1000).contains(&max_replicas) {
            return Err(Error::Configuration("invalid replica limit"));
        }
        let workspace = env::var("HOMELAB_EXECUTOR_WORKSPACE_POLICY_FILE").ok()
            .filter(|s| !s.is_empty()).map(|path| -> Result<WorkspacePolicy, Error> {
                let p: WorkspacePolicy = serde_json::from_slice(
                    &bounded_file(Path::new(&path), 64 << 10, false)?)
                    .map_err(|_| Error::Configuration("invalid Workspace policy"))?;
                p.validate()?;
                Ok(p)
            }).transpose()?;
        let workspace_token = env::var("HOMELAB_EXECUTOR_WORKSPACE_TOKEN").ok()
            .filter(|s| !s.is_empty()).map(PathBuf::from);
        let workspace_vault_alias = env::var("HOMELAB_EXECUTOR_WORKSPACE_VAULT_ALIAS").ok()
            .filter(|s| !s.is_empty());
        if workspace.is_some() != (workspace_token.is_some() ^ workspace_vault_alias.is_some())
            || workspace_token.as_ref().is_some_and(|p| !p.is_absolute())
            || workspace_vault_alias.as_ref().is_some_and(|s| !valid_workspace_name(s, false)) {
            return Err(Error::Configuration("Workspace requires exactly one credential source"));
        }
        let kubeconfig = env::var("HOMELAB_EXECUTOR_KUBECONFIG").ok()
            .filter(|s| !s.is_empty()).map(PathBuf::from);
        let kubectl = env::var("HOMELAB_EXECUTOR_KUBECTL").ok()
            .filter(|s| !s.is_empty()).map(PathBuf::from);
        if kubeconfig.is_some() != kubectl.is_some() || (deployments.is_empty() != kubeconfig.is_none()) {
            return Err(Error::Configuration("incomplete Kubernetes configuration"));
        }
        let health: Vec<String> = serde_json::from_slice(
            &bounded_file(&absolute("HOMELAB_EXECUTOR_HEALTH_FILE")?, 64 << 10, false)?)
            .map_err(|_| Error::Configuration("invalid health policy"))?;
        if health.is_empty() || health.len() > 32 || health.iter().any(|s| !valid_health_service(s)) {
            return Err(Error::Configuration("invalid release health policy"));
        }
        Ok(Self {
            host, release_enabled: env::var("HOMELAB_EXECUTOR_RELEASE_ENABLE").ok().as_deref() == Some("true"),
            systemctl: absolute("HOMELAB_EXECUTOR_SYSTEMCTL")?,
            actuator: absolute("HOMELAB_EXECUTOR_RUST_ACTUATOR")?,
            service_policy_file, deployment_policy_file,
            services, deployments, max_replicas, workspace, workspace_token, workspace_vault_alias,
            state_dir: absolute("HOMELAB_EXECUTOR_RELEASE_STATE")?,
            source: absolute("HOMELAB_EXECUTOR_SOURCE")?,
            nix: absolute("HOMELAB_EXECUTOR_NIX")?,
            nix_env: absolute("HOMELAB_EXECUTOR_NIX_ENV")?,
            git: absolute("HOMELAB_EXECUTOR_GIT")?,
            health, kubeconfig, kubectl,
        })
    }

    pub fn admit(&self, operation: &str, target: &str, fields: &Map<String, Value>) -> Result<Action, Error> {
        if target != self.host || fields.get("host").and_then(Value::as_str) != Some(target) {
            return Err(Error::Denied("target is not the current host"));
        }
        let service_verb = match operation {
            "infrastructure__service_start" => Some("start"),
            "infrastructure__service_stop" => Some("stop"),
            "infrastructure__service_reload" => Some("reload"),
            "infrastructure__service_restart" => Some("restart"),
            _ => None,
        };
        if let Some(verb) = service_verb {
            let service = fields.get("service").and_then(Value::as_str)
                .ok_or(Error::Denied("service identity required"))?;
            if fields.len() != 2 || !self.services.contains(service) {
                return Err(Error::Denied("service outside allowlist"));
            }
            return Ok(Action::Actuator(vec!["service".into(), verb.into(), service.into()]));
        }
        if matches!(operation,"cluster__deployment_restart"|"cluster__deployment_scale") {
            let namespace = fields.get("namespace").and_then(Value::as_str)
                .ok_or(Error::Denied("namespace required"))?;
            let deployment = fields.get("deployment").and_then(Value::as_str)
                .ok_or(Error::Denied("deployment required"))?;
            if !self.deployments.contains(&format!("{namespace}/{deployment}")) {
                return Err(Error::Denied("Kubernetes deployment not permitted"));
            }
            if operation == "cluster__deployment_restart" {
                if fields.len() != 3 { return Err(Error::Denied("unexpected deployment restart arguments")); }
                return Ok(Action::Actuator(vec!["deployment".into(),"restart".into(), namespace.into(), deployment.into()]));
            }
            if fields.len() != 4 && fields.len() != 5 {
                return Err(Error::Denied("unexpected deployment scale arguments"));
            }
            let replicas = fields.get("replicas").and_then(Value::as_u64)
                .ok_or(Error::Denied("replica count must be an integer"))?;
            if replicas > self.max_replicas as u64 {
                return Err(Error::Denied("replica count exceeds policy"));
            }
            if fields.len() == 5 && !fields.contains_key("resource_version") {
                return Err(Error::Denied("unexpected Kubernetes scale field"));
            }
            let mut args = vec!["deployment".into(),"scale".into(),namespace.into(),deployment.into(),replicas.to_string()];
            if let Some(version) = fields.get("resource_version") {
                let version = version.as_str().ok_or(Error::Denied("invalid Kubernetes resource version"))?;
                if version.is_empty() || version.len() > 128 || !version.as_bytes()[0].is_ascii_alphanumeric()
                    || !version.bytes().all(|c| c.is_ascii_alphanumeric() || b"._-".contains(&c)) {
                    return Err(Error::Denied("invalid Kubernetes version precondition"));
                }
                args.push(version.into());
            }
            return Ok(Action::Actuator(args));
        }
        if operation.starts_with("infrastructure__workspace_") {
            let policy = self.workspace.as_ref().ok_or(Error::Denied("Workspace administration disabled"))?;
            let (tool, arguments) = policy.operation(operation, fields)?;
            return Ok(Action::Workspace { tool, arguments });
        }
        if matches!(operation, "infrastructure__release_activate" | "infrastructure__release_revert") {
            if !self.release_enabled || fields.len() != 2 {
                return Err(Error::Denied("release activation disabled or invalid arguments"));
            }
            let id = fields.get("release_id").and_then(Value::as_str)
                .ok_or(Error::Denied("staged release ID required"))?;
            if !valid_hex(id,48) { return Err(Error::Denied("invalid release ID")); }
            return Ok(Action::Release { release_id: id.into(),
                activate: operation == "infrastructure__release_activate" });
        }
        Err(Error::Denied("unregistered privileged operation"))
    }
}
