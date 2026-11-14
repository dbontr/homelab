use super::{Engine, Error, Profile, Record};
use serde::{Deserialize, Serialize};

const MAX_SESSIONS: usize = 256;
const MAX_SESSIONS_PER_DAY: usize = 12;
pub(crate) fn initial_id() -> u64 {
    1
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum FocusStatus {
    Active,
    Completed,
    Abandoned,
    Superseded,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FocusSession {
    pub id: u64,
    pub goal_id: String,
    pub outcome: String,
    pub step: String,
    pub target_minutes: u16,
    pub started_at: u64,
    pub ended_at: Option<u64>,
    pub status: FocusStatus,
    pub reflection: String,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct FocusStart {
    pub target_minutes: u16,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct FocusFinish {
    pub id: u64,
    pub completed: bool,
    #[serde(default)]
    pub emergency: bool,
    #[serde(default)]
    pub reflection: String,
}

pub(crate) fn valid_sessions(sessions: &[FocusSession], next: u64) -> bool {
    next > 0
        && sessions.len() <= MAX_SESSIONS
        && sessions.windows(2).all(|w| w[0].id < w[1].id)
        && sessions
            .iter()
            .filter(|s| s.status == FocusStatus::Active)
            .count()
            <= 1
        && sessions.iter().all(|s| {
            s.id > 0
                && s.id < next
                && (10..=240).contains(&s.target_minutes)
                && s.goal_id.len() <= 128
                && s.outcome.chars().count() <= 400
                && s.step.chars().count() <= 320
                && s.reflection.chars().count() <= 400
                && !s.step.is_empty()
                && match (s.status, s.ended_at) {
                    (FocusStatus::Active, None) => true,
                    (FocusStatus::Active, Some(_)) => false,
                    (_, Some(end)) => end >= s.started_at,
                    (_, None) => false,
                }
        })
}

pub(crate) fn supersede_active(sessions: &mut [FocusSession], now: u64) {
    for session in sessions
        .iter_mut()
        .filter(|s| s.status == FocusStatus::Active)
    {
        session.status = FocusStatus::Superseded;
        session.ended_at = Some(now.max(session.started_at));
        session.reflection = "Goal revised".to_owned();
    }
}

impl Engine {
    pub fn start_focus(&mut self, request: FocusStart, now: u64) -> Result<FocusSession, Error> {
        if !(10..=240).contains(&request.target_minutes) {
            return Err(Error::Invalid(
                "focus duration must be between 10 and 240 minutes",
            ));
        }
        let profile: &Profile = &self.state.profile;
        if profile.identity.is_empty() || profile.outcome.is_empty() || profile.next_step.is_empty()
        {
            return Err(Error::Conflict("define an identity and next step first"));
        }
        if self
            .state
            .focus_sessions
            .iter()
            .any(|s| s.status == FocusStatus::Active)
        {
            return Err(Error::Conflict("an active focus commitment already exists"));
        }
        if self
            .state
            .focus_sessions
            .iter()
            .filter(|s| s.started_at / 86400 == now / 86400)
            .count()
            >= MAX_SESSIONS_PER_DAY
        {
            return Err(Error::Limited("daily focus commitment limit reached"));
        }
        let next_id = self
            .state
            .next_focus_id
            .checked_add(1)
            .ok_or(Error::Invalid("focus identity exhausted"))?;
        let session = FocusSession {
            id: self.state.next_focus_id,
            goal_id: profile.goal_id.clone(),
            outcome: profile.outcome.clone(),
            step: profile.next_step.clone(),
            target_minutes: request.target_minutes,
            started_at: now,
            ended_at: None,
            status: FocusStatus::Active,
            reflection: String::new(),
        };
        let mut next = self.state.clone();
        next.next_focus_id = next_id;
        next.focus_sessions.push(session.clone());
        if next.focus_sessions.len() > MAX_SESSIONS {
            next.focus_sessions.remove(0);
        }
        self.commit(next)?;
        Ok(session)
    }

    pub fn finish_focus(&mut self, request: FocusFinish, now: u64) -> Result<Record, Error> {
        if request.reflection.chars().count() > 400
            || request.reflection.chars().any(char::is_control)
        {
            return Err(Error::Invalid("invalid focus reflection"));
        }
        if !request.completed && !request.emergency && request.reflection.trim().chars().count() < 5
        {
            return Err(Error::Invalid(
                "a short reflection is required to end early",
            ));
        }
        let mut next = self.state.clone();
        let session = next
            .focus_sessions
            .iter_mut()
            .find(|s| s.id == request.id)
            .ok_or(Error::Invalid("focus commitment not found"))?;
        if session.status != FocusStatus::Active {
            return Err(Error::Conflict("focus commitment already closed"));
        }
        session.status = if request.completed {
            FocusStatus::Completed
        } else {
            FocusStatus::Abandoned
        };
        session.ended_at = Some(now.max(session.started_at));
        session.reflection = request.reflection.trim().to_owned();
        self.commit(next)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{Intensity, ProfileChange};
    use std::fs;
    use std::sync::atomic::{AtomicU64, Ordering};
    static COUNTER: AtomicU64 = AtomicU64::new(0);

    fn engine() -> (Engine, std::path::PathBuf) {
        let dir = std::env::temp_dir().join(format!(
            "motivation-focus-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&dir).unwrap();
        (Engine::open(dir.join("state.json")).unwrap(), dir)
    }
    fn configure(e: &mut Engine) {
        e.update_profile(ProfileChange {
            expected_revision: 0,
            enabled: false,
            proactive_enabled: false,
            cadence_minutes: 240,
            holdout_weekday_utc: None,
            goal_id: "study".into(),
            identity: "Mathematician".into(),
            outcome: "Develop a useful result".into(),
            next_step: "Prove a lemma".into(),
            intensity: Intensity::Direct,
            daily_limit: 4,
            quiet_start_utc_hour: 0,
            quiet_end_utc_hour: 0,
        })
        .unwrap();
    }
    #[test]
    fn focus_is_independent_of_notifications_and_prevents_parallel_sessions() {
        let (mut e, dir) = engine();
        configure(&mut e);
        let active = e
            .start_focus(FocusStart { target_minutes: 45 }, 100_000)
            .unwrap();
        assert!(matches!(
            e.start_focus(FocusStart { target_minutes: 25 }, 100_020),
            Err(Error::Conflict(_))
        ));
        assert!(matches!(
            e.finish_focus(
                FocusFinish {
                    id: active.id,
                    completed: false,
                    emergency: false,
                    reflection: String::new()
                },
                100_030
            ),
            Err(Error::Invalid(_))
        ));
        let closed = e
            .finish_focus(
                FocusFinish {
                    id: active.id,
                    completed: false,
                    emergency: false,
                    reflection: "Chose a different priority".into(),
                },
                100_400,
            )
            .unwrap();
        assert_eq!(closed.focus_sessions[0].status, FocusStatus::Abandoned);
        assert_eq!(e.metrics(100_500).focus_abandoned, 1);
        drop(e);
        let reopened = Engine::open(dir.join("state.json")).unwrap();
        assert_eq!(reopened.snapshot().focus_sessions.len(), 1);
        drop(reopened);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn goal_revisions_supersede_active_sessions_without_false_failure() {
        let (mut e, dir) = engine();
        configure(&mut e);
        let first = e
            .start_focus(FocusStart { target_minutes: 30 }, 100_000)
            .unwrap();
        e.update_profile(ProfileChange {
            expected_revision: 1,
            enabled: false,
            proactive_enabled: false,
            cadence_minutes: 240,
            holdout_weekday_utc: None,
            goal_id: "study".into(),
            identity: "Mathematician".into(),
            outcome: "Explore a new conjecture".into(),
            next_step: "Calculate spectral invariants".into(),
            intensity: Intensity::Direct,
            daily_limit: 4,
            quiet_start_utc_hour: 0,
            quiet_end_utc_hour: 0,
        })
        .unwrap();
        assert_eq!(
            e.snapshot().focus_sessions[0].status,
            FocusStatus::Superseded
        );
        assert!(matches!(
            e.finish_focus(
                FocusFinish {
                    id: first.id,
                    completed: true,
                    emergency: false,
                    reflection: String::new()
                },
                100_400
            ),
            Err(Error::Conflict(_))
        ));
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn focus_can_end_without_reflection_for_urgent_exit() {
        let (mut e, dir) = engine();
        configure(&mut e);
        let active = e
            .start_focus(FocusStart { target_minutes: 60 }, 100_000)
            .unwrap();
        e.finish_focus(
            FocusFinish {
                id: active.id,
                completed: false,
                emergency: true,
                reflection: String::new(),
            },
            100_001,
        )
        .unwrap();
        assert_eq!(e.metrics(100_050).focus_abandoned, 1);
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
}
