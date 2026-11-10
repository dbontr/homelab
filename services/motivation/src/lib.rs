use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

const MAX_EVENTS: usize = 256;
const MAX_BYTES: u64 = 2 * 1024 * 1024;
const COOLDOWN_SECONDS: u64 = 25 * 60;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Intensity {
    #[default]
    Guided,
    Direct,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Profile {
    pub revision: u64,
    pub enabled: bool,
    pub goal_id: String,
    pub identity: String,
    pub outcome: String,
    pub next_step: String,
    pub intensity: Intensity,
    pub daily_limit: u8,
    pub quiet_start_utc_hour: u8,
    pub quiet_end_utc_hour: u8,
}

impl Default for Profile {
    fn default() -> Self {
        Self {
            revision: 0,
            enabled: false,
            goal_id: String::new(),
            identity: String::new(),
            outcome: String::new(),
            next_step: String::new(),
            intensity: Intensity::Guided,
            daily_limit: 4,
            quiet_start_utc_hour: 0,
            quiet_end_utc_hour: 0,
        }
    }
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ProfileChange {
    pub expected_revision: u64,
    pub enabled: bool,
    #[serde(default)]
    pub goal_id: String,
    pub identity: String,
    pub outcome: String,
    pub next_step: String,
    pub intensity: Intensity,
    pub daily_limit: u8,
    pub quiet_start_utc_hour: u8,
    pub quiet_end_utc_hour: u8,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Strategy {
    Outcome,
    Inquiry,
    Challenge,
    Accountability,
    Reflection,
}

const STRATEGIES: [Strategy; 5] = [
    Strategy::Outcome,
    Strategy::Inquiry,
    Strategy::Challenge,
    Strategy::Accountability,
    Strategy::Reflection,
];

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Feedback {
    pub engagement: u8,
    pub progress: u8,
    pub wellbeing: u8,
    pub notes: String,
    pub recorded_at: u64,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct FeedbackInput {
    pub intervention_id: u64,
    pub engagement: u8,
    pub progress: u8,
    pub wellbeing: u8,
    #[serde(default)]
    pub notes: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Intervention {
    pub id: u64,
    pub strategy: Strategy,
    pub message: String,
    pub created_at: u64,
    pub feedback: Option<Feedback>,
    #[serde(default)]
    pub cancelled: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Record {
    pub profile: Profile,
    pub interventions: Vec<Intervention>,
    pub next_id: u64,
}

impl Default for Record {
    fn default() -> Self {
        Self {
            profile: Profile::default(),
            interventions: Vec::new(),
            next_id: 1,
        }
    }
}

#[derive(Debug)]
pub enum Error {
    Invalid(&'static str),
    Conflict(&'static str),
    Limited(&'static str),
    Io(std::io::Error),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(message) | Self::Conflict(message) | Self::Limited(message) => {
                write!(f, "{message}")
            }
            Self::Io(_) => write!(f, "private motivation state unavailable"),
        }
    }
}

impl std::error::Error for Error {}
impl From<std::io::Error> for Error {
    fn from(value: std::io::Error) -> Self { Self::Io(value) }
}

pub fn unix_seconds() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

pub struct Engine {
    path: PathBuf,
    state: Record,
    _lock: File,
}

impl Engine {
    pub fn open(path: impl AsRef<Path>) -> Result<Self, Error> {
        let path = path.as_ref();
        if !path.is_absolute() {
            return Err(Error::Invalid("state file must be an absolute path"));
        }
        let dir = path.parent().ok_or(Error::Invalid("state directory required"))?;
        if fs::symlink_metadata(dir)?.file_type().is_symlink() || !dir.is_dir() {
            return Err(Error::Invalid("state directory must not be a symlink"));
        }
        if let Ok(meta) = fs::symlink_metadata(path) {
            if !meta.is_file() || meta.file_type().is_symlink() ||
                meta.permissions().mode() & 0o077 != 0 {
                return Err(Error::Invalid("state file must be a private regular file"));
            }
        }
        let lock_path = path.with_extension("lock");
        if let Ok(meta) = fs::symlink_metadata(&lock_path) {
            if !meta.is_file() || meta.file_type().is_symlink() ||
                meta.permissions().mode() & 0o077 != 0 {
                return Err(Error::Invalid("lock file must be a private regular file"));
            }
        }
        let lock = OpenOptions::new()
            .read(true).write(true).create(true).mode(0o600)
            .custom_flags(libc::O_NOFOLLOW).open(lock_path)?;
        lock.try_lock_exclusive()?;
        let state = match OpenOptions::new()
            .read(true).custom_flags(libc::O_NOFOLLOW).open(path) {
            Ok(mut file) => {
                if file.metadata()?.len() > MAX_BYTES {
                    return Err(Error::Invalid("state file exceeds the size limit"));
                }
                let mut bytes = Vec::new();
                file.read_to_end(&mut bytes)?;
                serde_json::from_slice::<Record>(&bytes)
                    .map_err(|_| Error::Invalid("state file is invalid"))?
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Record::default(),
            Err(e) => return Err(e.into()),
        };
        validate_record(&state)?;
        Ok(Self { path: path.to_owned(), state, _lock: lock })
    }

    pub fn snapshot(&self) -> Record { self.state.clone() }

    fn commit(&mut self, state: Record) -> Result<Record, Error> {
        validate_record(&state)?;
        let data = serde_json::to_vec_pretty(&state)
            .map_err(|_| Error::Invalid("could not encode state"))?;
        if data.len() as u64 > MAX_BYTES {
            return Err(Error::Invalid("state capacity exceeded"));
        }
        let parent = self.path.parent().ok_or(Error::Invalid("state directory required"))?;
        let temp = parent.join(format!(
            ".motivation-{}-{}.tmp",
            std::process::id(),
            SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos()
        ));
        let result = (|| -> Result<(), Error> {
            let mut file = OpenOptions::new()
                .write(true).create_new(true).mode(0o600)
                .custom_flags(libc::O_NOFOLLOW).open(&temp)?;
            file.write_all(&data)?;
            file.sync_all()?;
            fs::rename(&temp, &self.path)?;
            File::open(parent)?.sync_all()?;
            Ok(())
        })();
        if result.is_err() {
            let _ = fs::remove_file(&temp);
            return result.map(|()| state);
        }
        self.state = state.clone();
        Ok(state)
    }

    pub fn update_profile(&mut self, change: ProfileChange) -> Result<Record, Error> {
        if change.expected_revision != self.state.profile.revision {
            return Err(Error::Conflict("profile revision conflict"));
        }
        let profile = Profile {
            revision: self.state.profile.revision.checked_add(1)
                .ok_or(Error::Invalid("profile revision exhausted"))?,
            enabled: change.enabled,
            goal_id: change.goal_id.trim().to_owned(),
            identity: change.identity.trim().to_owned(),
            outcome: change.outcome.trim().to_owned(),
            next_step: change.next_step.trim().to_owned(),
            intensity: change.intensity,
            daily_limit: change.daily_limit,
            quiet_start_utc_hour: change.quiet_start_utc_hour,
            quiet_end_utc_hour: change.quiet_end_utc_hour,
        };
        validate_profile(&profile)?;
        let mut next = self.state.clone();
        next.profile = profile;
        for event in &mut next.interventions {
            if event.feedback.is_none() { event.cancelled = true; }
        }
        self.commit(next)
    }

    pub fn pause(&mut self) -> Result<Record, Error> {
        let mut next = self.state.clone();
        next.profile.enabled = false;
        for event in &mut next.interventions {
            if event.feedback.is_none() { event.cancelled = true; }
        }
        next.profile.revision = next.profile.revision.checked_add(1)
            .ok_or(Error::Invalid("profile revision exhausted"))?;
        self.commit(next)
    }

    pub fn suggest(&mut self, now: u64) -> Result<Intervention, Error> {
        let p = &self.state.profile;
        if !p.enabled {
            return Err(Error::Conflict("motivation interventions are paused"));
        }
        if let Some(pending) = self.state.interventions.iter().rev()
            .find(|event| event.feedback.is_none() && !event.cancelled) {
            return Ok(pending.clone());
        }
        let hour = (now / 3600 % 24) as u8;
        let quiet = if p.quiet_start_utc_hour < p.quiet_end_utc_hour {
            hour >= p.quiet_start_utc_hour && hour < p.quiet_end_utc_hour
        } else if p.quiet_start_utc_hour > p.quiet_end_utc_hour {
            hour >= p.quiet_start_utc_hour || hour < p.quiet_end_utc_hour
        } else { false };
        if quiet {
            return Err(Error::Limited("interventions are disabled during quiet hours"));
        }
        let today = now / 86400;
        if self.state.interventions.iter()
            .filter(|event| event.created_at / 86400 == today).count() >= p.daily_limit as usize {
            return Err(Error::Limited("daily intervention limit reached"));
        }
        if self.state.interventions.last()
            .is_some_and(|last| now.saturating_sub(last.created_at) < COOLDOWN_SECONDS) {
            return Err(Error::Limited("intervention cooldown is active"));
        }
        let strategy = select_strategy(&self.state.interventions);
        let next_id = self.state.next_id.checked_add(1)
            .ok_or(Error::Invalid("intervention identity exhausted"))?;
        let event = Intervention {
            id: self.state.next_id,
            strategy,
            message: format_message(p, strategy),
            created_at: now,
            feedback: None,
            cancelled: false,
        };
        let mut next = self.state.clone();
        next.next_id = next_id;
        next.interventions.push(event.clone());
        if next.interventions.len() > MAX_EVENTS {
            next.interventions.remove(0);
        }
        self.commit(next)?;
        Ok(event)
    }

    pub fn record_feedback(&mut self, input: FeedbackInput, now: u64) -> Result<Record, Error> {
        if input.engagement > 5 || input.progress > 5 || input.wellbeing > 5 ||
            input.notes.chars().count() > 400 {
            return Err(Error::Invalid("feedback values are outside accepted bounds"));
        }
        let mut next = self.state.clone();
        let event = next.interventions.iter_mut()
            .find(|event| event.id == input.intervention_id)
            .ok_or(Error::Invalid("intervention not found"))?;
        if event.cancelled || event.feedback.is_some() {
            return Err(Error::Conflict("intervention already resolved"));
        }
        event.feedback = Some(Feedback {
            engagement: input.engagement,
            progress: input.progress,
            wellbeing: input.wellbeing,
            notes: input.notes.trim().to_owned(),
            recorded_at: now,
        });
        if input.wellbeing <= 1 {
            next.profile.enabled = false;
            next.profile.revision = next.profile.revision.checked_add(1)
                .ok_or(Error::Invalid("profile revision exhausted"))?;
        }
        self.commit(next)
    }
}

fn validate_profile(p: &Profile) -> Result<(), Error> {
    if p.identity.chars().count() > 240 || p.outcome.chars().count() > 400 ||
        p.next_step.chars().count() > 320 || p.goal_id.len() > 128 {
        return Err(Error::Invalid("profile text exceeds accepted limits"));
    }
    if p.goal_id.chars().any(|c| !c.is_ascii_alphanumeric() && c != '-' && c != '_') {
        return Err(Error::Invalid("invalid goal reference"));
    }
    if p.daily_limit == 0 || p.daily_limit > 8 ||
        p.quiet_start_utc_hour > 23 || p.quiet_end_utc_hour > 23 {
        return Err(Error::Invalid("invalid intervention limits"));
    }
    if p.enabled && (p.identity.is_empty() || p.outcome.is_empty() || p.next_step.is_empty()) {
        return Err(Error::Invalid("identity, outcome and next step are required before enabling"));
    }
    Ok(())
}

fn validate_record(record: &Record) -> Result<(), Error> {
    validate_profile(&record.profile)?;
    if record.interventions.len() > MAX_EVENTS || record.next_id == 0 ||
        record.interventions.iter().any(|e| e.id >= record.next_id ||
            e.message.chars().count() > 1600 ||
            e.feedback.as_ref().is_some_and(|f| f.engagement > 5 ||
                f.progress > 5 || f.wellbeing > 5 || f.notes.chars().count() > 400)) {
        return Err(Error::Invalid("stored intervention history is invalid"));
    }
    if record.interventions.windows(2).any(|w| w[0].id >= w[1].id) ||
        record.interventions.iter().filter(|e| e.feedback.is_none() && !e.cancelled).count() > 1 ||
        record.interventions.iter().any(|e| e.cancelled && e.feedback.is_some()) {
        return Err(Error::Invalid("stored intervention sequence is invalid"));
    }
    Ok(())
}

fn select_strategy(events: &[Intervention]) -> Strategy {
    let completed: Vec<&Intervention> = events.iter().filter(|e| e.feedback.is_some()).collect();
    let total = completed.len();
    let mut best = Strategy::Outcome;
    let mut best_value = f64::NEG_INFINITY;
    for strategy in STRATEGIES {
        let matches: Vec<_> = completed.iter().filter(|e| e.strategy == strategy).collect();
        if matches.is_empty() { return strategy; }
        let average = matches.iter().map(|event| {
            let feedback = event.feedback.as_ref().expect("completed intervention");
            (feedback.progress as f64 * 0.50 +
             feedback.engagement as f64 * 0.25 +
             feedback.wellbeing as f64 * 0.25) / 5.0
        }).sum::<f64>() / matches.len() as f64;
        let exploration = (2.0 * (total as f64).ln() / matches.len() as f64).sqrt();
        let value = average + exploration;
        if value > best_value {
            best_value = value;
            best = strategy;
        }
    }
    best
}

fn format_message(p: &Profile, strategy: Strategy) -> String {
    let opening = match strategy {
        Strategy::Outcome => format!(
            "Your chosen objective is: {}. What tangible evidence would move you toward that result today?",
            p.outcome
        ),
        Strategy::Inquiry => format!(
            "What is the most interesting unanswered question behind this outcome: {}? Begin by identifying what you do not yet know.",
            p.outcome
        ),
        Strategy::Challenge => format!(
            "Give this a serious, uninterrupted 25-minute attempt: {}. Record your reasoning, not merely the answer.",
            p.next_step
        ),
        Strategy::Accountability => format!(
            "You defined your desired identity as: {}. Compare your present intention with that commitment. What action follows?",
            p.identity
        ),
        Strategy::Reflection => format!(
            "Your aim is: {}. Choose one concrete result you can establish today, then decide what evidence will count.",
            p.outcome
        ),
    };
    if p.intensity == Intensity::Direct {
        format!("{opening} Your next committed step is: {}. Choose deliberately: begin it, or revise the commitment.", p.next_step)
    } else {
        format!("{opening} A useful next step is: {}.", p.next_step)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};
    static COUNTER: AtomicU64 = AtomicU64::new(0);

    fn engine() -> (Engine, PathBuf) {
        let path = std::env::temp_dir().join(format!(
            "homelab-motivation-test-{}-{}", std::process::id(), COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&path).unwrap();
        let engine = Engine::open(path.join("state.json")).unwrap();
        (engine, path)
    }
    fn configured(engine: &mut Engine) {
        engine.update_profile(ProfileChange {
            expected_revision: 0, enabled: true, goal_id: "research".into(),
            identity: "Independent researcher".into(),
            outcome: "Test a meaningful scientific conjecture".into(),
            next_step: "Derive the governing equation".into(),
            intensity: Intensity::Direct, daily_limit: 4,
            quiet_start_utc_hour: 0, quiet_end_utc_hour: 0,
        }).unwrap();
    }
    #[test]
    fn opt_in_and_revision_are_enforced() {
        let (mut e, dir) = engine();
        assert!(matches!(e.suggest(100_000), Err(Error::Conflict(_))));
        configured(&mut e);
        assert!(matches!(e.update_profile(ProfileChange {
            expected_revision: 0, enabled: true, goal_id: String::new(),
            identity: "x".into(), outcome: "x".into(), next_step: "x".into(),
            intensity: Intensity::Guided, daily_limit: 3,
            quiet_start_utc_hour: 0, quiet_end_utc_hour: 0,
        }), Err(Error::Conflict(_))));
        e.pause().unwrap();
        assert!(matches!(e.suggest(100_000), Err(Error::Conflict(_))));
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn intervention_limited_and_feedback_durable() {
        let (mut e, dir) = engine();
        configured(&mut e);
        let first = e.suggest(100_000).unwrap();
        assert_eq!(first.strategy, Strategy::Outcome);
        assert_eq!(e.suggest(100_001).unwrap().id, first.id);
        e.record_feedback(FeedbackInput {
            intervention_id: first.id, engagement: 4, progress: 3,
            wellbeing: 5, notes: "solved a subproblem".into(),
        }, 100_050).unwrap();
        assert!(matches!(e.suggest(100_060), Err(Error::Limited(_))));
        let next = e.suggest(102_000).unwrap();
        assert_eq!(next.strategy, Strategy::Inquiry);
        drop(e);
        let reopened = Engine::open(dir.join("state.json")).unwrap();
        assert_eq!(reopened.snapshot().interventions.len(), 2);
        assert_eq!(reopened.snapshot().interventions[0].feedback.as_ref().unwrap().progress, 3);
        drop(reopened);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn changed_identity_cancels_outdated_intervention() {
        let (mut e, dir) = engine();
        configured(&mut e);
        let old = e.suggest(100_000).unwrap();
        e.update_profile(ProfileChange {
            expected_revision: 1, enabled: true, goal_id: "research".into(),
            identity: "Mathematician".into(), outcome: "Prove a theorem".into(),
            next_step: "Investigate an open lemma".into(),
            intensity: Intensity::Guided, daily_limit: 4,
            quiet_start_utc_hour: 0, quiet_end_utc_hour: 0,
        }).unwrap();
        assert!(e.snapshot().interventions[0].cancelled);
        assert!(matches!(e.record_feedback(FeedbackInput {
            intervention_id: old.id, engagement: 5, progress: 5, wellbeing: 5,
            notes: String::new(),
        }, 100_010), Err(Error::Conflict(_))));
        let next = e.suggest(102_000).unwrap();
        assert_ne!(next.id, old.id);
        assert!(next.message.contains("Prove a theorem"));
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn negative_wellbeing_pauses_without_destroying_history() {
        let (mut e, dir) = engine();
        configured(&mut e);
        let event = e.suggest(100_000).unwrap();
        let status = e.record_feedback(FeedbackInput {
            intervention_id: event.id, engagement: 5, progress: 5, wellbeing: 1, notes: String::new(),
        }, 100_050).unwrap();
        assert!(!status.profile.enabled);
        assert_eq!(status.profile.revision, 2);
        assert_eq!(status.interventions.len(), 1);
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn daily_limits_and_quiet_hours_are_enforced() {
        let (mut e, dir) = engine();
        configured(&mut e);
        let first = e.suggest(86400 + 3600).unwrap();
        e.record_feedback(FeedbackInput {
            intervention_id: first.id, engagement: 3, progress: 3,
            wellbeing: 3, notes: String::new(),
        }, 86400 + 3610).unwrap();
        e.update_profile(ProfileChange {
            expected_revision: 1, enabled: true, goal_id: "research".into(),
            identity: "Researcher".into(), outcome: "Publish sound work".into(),
            next_step: "Prove a lemma".into(), intensity: Intensity::Guided,
            daily_limit: 1, quiet_start_utc_hour: 22, quiet_end_utc_hour: 6,
        }).unwrap();
        assert!(matches!(e.suggest(86400 + 23 * 3600), Err(Error::Limited(_))));
        assert!(matches!(e.suggest(86400 + 12 * 3600), Err(Error::Limited(_))));
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
}
