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
    #[serde(default, rename = "ImageReferences")]
    pub image_references: Vec<String>,
    #[serde(rename = "Software")]
    pub software: Vec<String>,
    #[serde(default, rename = "Programs")]
    pub programs: Vec<String>,
    #[serde(rename = "Packages")]
    pub packages: Vec<String>,
}

// The executor accepts an exact, version-independent controller request.
// Unknown fields cannot broaden resource allocation authority.
#[derive(serde::Deserialize, serde::Serialize)]
#[serde(deny_unknown_fields)]
struct WorkspaceAllocation {
    host: String,
    project_id: String,
    image_pool_id: String,
    #[serde(default)]
    node_id: String,
    #[serde(default)]
    architecture: String,
    cpu: f64,
    memory_mb: u32,
    duration_minutes: u32,
    #[serde(default)]
    hardware: std::collections::BTreeMap<String,u32>,
    #[serde(default)]
    min_gpu_memory_mb: u32,
    #[serde(default)]
    usb_passthrough: bool,
    #[serde(default)]
    usb_node: String,
    #[serde(default)]
    usb_device_id: String,
    #[serde(default)]
    display: bool,
    #[serde(default)]
    display_width: u32,
    #[serde(default)]
    display_height: u32,
}

impl WorkspaceAllocation {
    fn checked(fields: &Map<String,Value>, policy:&WorkspacePolicy)
        -> Result<Value,Error>
    {
        if serde_json::to_vec(fields).map_err(|_|Error::Denied("invalid workspace JSON"))?
            .len()>16<<10 {
            return Err(Error::Denied("workspace allocation exceeds request limit"));
        }
        let mut input: Self=serde_json::from_value(Value::Object(fields.clone()))
            .map_err(|_|Error::Denied("workspace allocation contains invalid fields"))?;
        if !valid_workspace_name(&input.host,false)
            || !policy.projects.contains(&input.project_id)
            || !policy.images.contains(&input.image_pool_id)
            || !valid_workspace_name(&input.image_pool_id,false)
            || !input.node_id.is_empty()&&
                (!valid_workspace_name(&input.node_id,false)
                    || !policy.workers.contains(&input.node_id))
            || !input.architecture.is_empty()&&
                input.architecture!="amd64"&&input.architecture!="arm64"
            || !input.cpu.is_finite() || !(0.1..=32.0).contains(&input.cpu)
            || !(64..=131072).contains(&input.memory_mb)
            || !(1..=1440).contains(&input.duration_minutes)
            || input.min_gpu_memory_mb>1_000_000
            || input.hardware.len()>4
        {
            return Err(Error::Denied("workspace allocation is outside operator policy"));
        }
        for (kind,count) in &input.hardware {
            if !matches!(kind.as_str(),"gpu"|"npu"|"fpga"|"usb")
                || !(1..=2).contains(count) {
                return Err(Error::Denied("unsupported hardware allocation"));
            }
        }
        if input.min_gpu_memory_mb>0&&input.hardware.get("gpu").copied().unwrap_or(0)==0{
            return Err(Error::Denied("GPU memory requires an approved GPU"));
        }
        if input.usb_passthrough {
            let id=&input.usb_device_id;
            if input.hardware.get("usb").copied().unwrap_or(0)>1
                || input.usb_node.is_empty()
                || !policy.workers.contains(&input.usb_node)
                || !valid_workspace_name(&input.usb_node,false)
                || !input.node_id.is_empty()&&input.node_id!=input.usb_node
                || id.is_empty()||id.len()>128
                || !id.as_bytes()[0].is_ascii_alphanumeric()
                || !id.bytes().all(|b|b.is_ascii_alphanumeric()
                    ||matches!(b,b'.'|b'_'|b':'|b'-'))
            {
                return Err(Error::Denied("USB requires an exact approved worker and device"));
            }
            input.hardware.insert("usb".into(),1);
        } else if input.hardware.contains_key("usb")
            || !input.usb_node.is_empty()||!input.usb_device_id.is_empty() {
            return Err(Error::Denied("USB passthrough is not enabled"));
        }
        if input.display {
            if !(320..=3840).contains(&input.display_width)
                || !(240..=2160).contains(&input.display_height)
                || input.display_width*input.display_height>8_000_000
            {
                return Err(Error::Denied("display dimensions are outside policy"));
            }
        } else if input.display_width!=0 || input.display_height!=0 {
            return Err(Error::Denied("display dimensions require explicit opt-in"));
        }
        let mut result=serde_json::to_value(input)
            .map_err(|_|Error::Denied("invalid workspace allocation"))?;
        result.as_object_mut().ok_or(Error::Denied("invalid allocation record"))?
            .remove("host");
        Ok(result)
    }
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
        let all = [&self.projects, &self.workers, &self.images, &self.software, &self.packages,
            &self.programs];
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
        if self.image_references.len()>64 {
            return Err(Error::Configuration("Workspace image reference policy exceeds limit"));
        }
        let mut seen = BTreeSet::new();
        for reference in &self.image_references {
            if !valid_workspace_image_reference(reference) || !seen.insert(reference) {
                return Err(Error::Configuration("Workspace image references must be unique immutable digests"));
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
            "infrastructure__workspace_create" => {
                let request = WorkspaceAllocation::checked(fields,self)?;
                ("workspace_create",request)
            }
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

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn workspace_allocation_is_limited_to_operator_resources() {
        let policy = WorkspacePolicy {
            endpoint: "https://workspace.example/mcp".into(),
            projects: vec!["research".into()],
            workers: vec!["jupiter".into()],
            images: vec!["approved-image".into()],
            image_references: Vec::new(),
            software: Vec::new(),
            programs: Vec::new(),
            packages: Vec::new(),
        };
        let accepted = json!({
            "host":"saturn",
            "project_id":"research",
            "image_pool_id":"approved-image",
            "node_id":"jupiter",
            "cpu":2.0,
            "memory_mb":2048,
            "duration_minutes":90,
            "hardware":{"gpu":1}
        });
        let fields = accepted.as_object().unwrap();
        let (name, args) = policy.operation("infrastructure__workspace_create",fields)
            .expect("allowlisted workspace");
        assert_eq!(name,"workspace_create");
        assert_eq!(args["project_id"],"research");
        assert!(args.get("host").is_none());
        for (key,value) in [
            ("project_id",json!("other")),
            ("image_pool_id",json!("unapproved")),
            ("node_id",json!("untrusted")),
            ("cpu",json!(200)),
            ("memory_mb",json!(0)),
            ("architecture",json!("invalid")),
            ("hardware",json!({"sudo":1})),
            ("hardware",json!({"gpu":9})),
            ("unknown_field",json!(true)),
        ] {
            let mut changed=fields.clone();
            changed.insert(key.into(),value);
            assert!(policy.operation("infrastructure__workspace_create",&changed).is_err(),
                "unsafe allocation accepted: {key}");
        }
        let mut usb=fields.clone();
        usb.insert("hardware".into(),json!({"usb":1}));
        usb.insert("usb_passthrough".into(),json!(true));
        usb.insert("usb_node".into(),json!("jupiter"));
        usb.insert("usb_device_id".into(),json!("jtag"));
        assert!(policy.operation("infrastructure__workspace_create",&usb).is_ok());
        usb.insert("usb_node".into(),json!("outside"));
        assert!(policy.operation("infrastructure__workspace_create",&usb).is_err());
    }

    fn policy() -> Policy {
        Policy {
            host: "jupiter".into(),
            release_enabled: true,
            systemctl: "/run/current-system/sw/bin/systemctl".into(),
            actuator: "/nix/store/fixture/bin/homelab-actuator".into(),
            service_policy_file: "/nix/store/fixture/services.txt".into(),
            deployment_policy_file: "/nix/store/fixture/deployments.txt".into(),
            services: ["homelab-platform.service".into()].into_iter().collect(),
            deployments: ["apps/controller".into()].into_iter().collect(),
            max_replicas: 4,
            workspace: None,
            workspace_token: None,
            workspace_vault_alias: None,
            state_dir: "/var/lib/homelab-releases".into(),
            source: "/srv/homelab".into(),
            nix: "/run/current-system/sw/bin/nix".into(),
            nix_env: "/run/current-system/sw/bin/nix-env".into(),
            git: "/run/current-system/sw/bin/git".into(),
            health: vec!["homelab-platform.service".into()],
            kubeconfig: None,
            kubectl: None,
        }
    }

    fn fields(value: Value) -> Map<String, Value> {
        value.as_object().unwrap().clone()
    }

    #[test]
    fn service_policy_is_exact_and_host_bound() {
        let policy = policy();
        let args = fields(json!({"host":"jupiter","service":"homelab-platform.service"}));
        assert!(matches!(
            policy.admit("infrastructure__service_restart","jupiter",&args),
            Ok(Action::Actuator(_))
        ));
        for (operation,target,value) in [
            ("infrastructure__service_restart","venus",json!({"host":"jupiter","service":"homelab-platform.service"})),
            ("infrastructure__service_restart","jupiter",json!({"host":"jupiter","service":"nginx.service"})),
            ("infrastructure__service_restart","jupiter",json!({"host":"jupiter","service":"homelab-platform.service","force":true})),
            ("infrastructure__service_destroy","jupiter",json!({"host":"jupiter","service":"homelab-platform.service"})),
        ] {
            assert!(policy.admit(operation,target,&fields(value)).is_err());
        }
    }

    #[test]
    fn kubernetes_admission_binds_namespace_replica_count_and_version() {
        let policy = policy();
        let allowed = fields(json!({"host":"jupiter","namespace":"apps",
            "deployment":"controller","replicas":3,"resource_version":"12345"}));
        assert!(matches!(policy.admit("cluster__deployment_scale","jupiter",&allowed),
            Ok(Action::Actuator(args)) if args.last().map(String::as_str) == Some("12345")));
        for value in [
            json!({"host":"jupiter","namespace":"apps","deployment":"controller","replicas":5}),
            json!({"host":"jupiter","namespace":"kube-system","deployment":"controller","replicas":1}),
            json!({"host":"jupiter","namespace":"apps","deployment":"controller","replicas":1.5}),
            json!({"host":"jupiter","namespace":"apps","deployment":"controller","replicas":2,
                "resource_version":"--force"}),
            json!({"host":"jupiter","namespace":"apps","deployment":"controller","replicas":2,
                "extra":"malformed"}),
        ] {
            assert!(policy.admit("cluster__deployment_scale","jupiter",&fields(value)).is_err());
        }
    }

    #[test]
    fn release_policy_accepts_only_existing_id_shape() {
        let policy = policy();
        let id = "a".repeat(48);
        let args = fields(json!({"host":"jupiter","release_id":id}));
        assert!(matches!(
            policy.admit("infrastructure__release_activate","jupiter",&args),
            Ok(Action::Release { activate: true, .. })
        ));
        assert!(policy.admit("infrastructure__release_activate","jupiter",
            &fields(json!({"host":"jupiter","release_id":"../../system"}))).is_err());
        assert!(policy.admit("infrastructure__release_status","jupiter",&args).is_err());
    }

    #[test]
    fn workspace_administration_is_disabled_without_a_scoped_provider() {
        let policy = policy();
        let args = fields(json!({"host":"jupiter","project_id":"research",
            "workspace_id":"workspace1"}));
        assert!(policy.admit("infrastructure__workspace_stop","jupiter",&args).is_err());
    }
}
