use super::{Engine, Error, Intervention, MAX_CHECKINS};
use serde::{Deserialize, Serialize};

const DAYS: u64 = 28;
const MAX_PER_DAY: usize = 8;

fn utc_weekday(timestamp: u64) -> u8 { ((timestamp / 86400 + 4) % 7) as u8 }

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Checkin {
    pub recorded_at: u64,
    pub minutes: u16,
    pub self_initiated: bool,
    pub fascination: u8,
    pub wellbeing: u8,
    pub intervention_id: Option<u64>,
    pub evidence_ref: String,
    #[serde(default)]
    pub holdout: bool,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct CheckinInput {
    pub minutes: u16,
    pub self_initiated: bool,
    pub fascination: u8,
    pub wellbeing: u8,
    #[serde(default)]
    pub intervention_id: Option<u64>,
    #[serde(default)]
    pub evidence_ref: String,
}

#[derive(Clone, Debug, Default, Serialize)]
pub struct EngagementSummary {
    pub days: u64,
    pub sessions: usize,
    pub minutes: u64,
    pub self_initiated_sessions: usize,
    pub self_initiated_minutes: u64,
    pub holdout_minutes: u64,
    pub holdout_self_initiated_minutes: u64,
    pub mean_fascination: f64,
    pub mean_wellbeing: f64,
    pub interventions_with_feedback: usize,
    pub focus_completed: usize,
    pub focus_abandoned: usize,
    pub focus_elapsed_minutes: u64,
}

// Check-ins are observations made by the owner, not independent evidence of mastery.
pub(crate) fn valid_checkins(checkins: &[Checkin], next_id: u64) -> bool {
    checkins.iter().all(|c| {
        c.minutes > 0 && c.minutes <= 720 && c.fascination <= 5 && c.wellbeing <= 5 &&
        c.evidence_ref.len() <= 128 &&
        c.evidence_ref.chars().all(|ch|
            ch.is_ascii_alphanumeric() || matches!(ch, ':' | '/' | '_' | '-')) &&
        c.intervention_id.is_none_or(|id| id > 0 && id < next_id)
    }) && checkins.windows(2).all(|w| w[0].recorded_at <= w[1].recorded_at)
}

impl Engine {
    // A scheduled intervention lives in the owner's inbox. Delivery to third parties
    // and external notifications are not authorized by enabling this feature.
    pub fn tick(&mut self, now: u64) -> Result<Option<Intervention>, Error> {
        let policy = &self.state.profile;
        if !policy.enabled || !policy.proactive_enabled { return Ok(None); }
        if policy.holdout_weekday_utc == Some(utc_weekday(now)) { return Ok(None); }
        if self.state.interventions.iter()
            .any(|event| event.feedback.is_none() && !event.cancelled) { return Ok(None); }
        if self.state.interventions.last().is_some_and(|event|
            now.saturating_sub(event.created_at) <
            u64::from(policy.cadence_minutes) * 60) { return Ok(None); }
        match self.suggest(now) {
            Ok(event) => Ok(Some(event)),
            Err(Error::Limited(_)) => Ok(None),
            Err(err) => Err(err),
        }
    }

    pub fn dismiss(&mut self, id: u64) -> Result<super::Record, Error> {
        let mut next = self.state.clone();
        let event = next.interventions.iter_mut().find(|e| e.id == id)
            .ok_or(Error::Invalid("intervention not found"))?;
        if event.feedback.is_some() || event.cancelled {
            return Err(Error::Conflict("intervention already resolved"));
        }
        event.cancelled = true;
        self.commit(next)
    }

    pub fn checkin(&mut self, input: CheckinInput, now: u64) -> Result<super::Record, Error> {
        let proposed = Checkin {
            recorded_at: now,
            minutes: input.minutes,
            self_initiated: input.self_initiated,
            fascination: input.fascination,
            wellbeing: input.wellbeing,
            intervention_id: input.intervention_id,
            evidence_ref: input.evidence_ref.trim().to_owned(),
            holdout: self.state.profile.holdout_weekday_utc == Some(utc_weekday(now)) &&
                !self.state.interventions.iter().any(|i|
                    (i.created_at / 86400 == now / 86400) ||
                    (!i.cancelled && i.feedback.is_none() && i.created_at < now)),
        };
        if !valid_checkins(std::slice::from_ref(&proposed), self.state.next_id) {
            return Err(Error::Invalid("invalid engagement check-in"));
        }
        if let Some(id) = proposed.intervention_id {
            if !self.state.interventions.iter().any(|e| e.id == id && !e.cancelled) {
                return Err(Error::Invalid("invalid intervention reference"));
            }
        }
        if self.state.checkins.last().is_some_and(|last| now < last.recorded_at) {
            return Err(Error::Conflict("check-in timestamp moved backward"));
        }
        if self.state.checkins.iter().filter(|c|
            c.recorded_at / 86400 == now / 86400).count() >= MAX_PER_DAY {
            return Err(Error::Limited("daily check-in limit reached"));
        }
        let mut next = self.state.clone();
        next.checkins.push(proposed);
        if next.checkins.len() > MAX_CHECKINS { next.checkins.remove(0); }
        if input.wellbeing <= 1 && next.profile.enabled {
            next.profile.enabled = false;
            next.profile.proactive_enabled = false;
            next.profile.revision = next.profile.revision.checked_add(1)
                .ok_or(Error::Invalid("profile revision exhausted"))?;
            for event in &mut next.interventions {
                if event.feedback.is_none() { event.cancelled = true; }
            }
        }
        self.commit(next)
    }

    pub fn metrics(&self, now: u64) -> EngagementSummary {
        let start = now.saturating_sub(DAYS * 86400);
        let recent: Vec<_> = self.state.checkins.iter()
            .filter(|c| c.recorded_at >= start && c.recorded_at <= now).collect();
        let n = recent.len();
        let mut summary = EngagementSummary {
            days: DAYS,
            sessions: n,
            minutes: recent.iter().map(|c| u64::from(c.minutes)).sum(),
            self_initiated_sessions: recent.iter().filter(|c| c.self_initiated).count(),
            self_initiated_minutes: recent.iter().filter(|c| c.self_initiated)
                .map(|c| u64::from(c.minutes)).sum(),
            holdout_minutes: recent.iter().filter(|c| c.holdout)
                .map(|c| u64::from(c.minutes)).sum(),
            holdout_self_initiated_minutes: recent.iter()
                .filter(|c| c.holdout && c.self_initiated)
                .map(|c| u64::from(c.minutes)).sum(),
            mean_fascination: 0.0,
            mean_wellbeing: 0.0,
            interventions_with_feedback: self.state.interventions.iter()
                .filter(|event| event.feedback.as_ref().is_some_and(|f|
                    f.recorded_at >= start && f.recorded_at <= now)).count(),
            focus_completed: self.state.focus_sessions.iter().filter(|s|
                s.started_at >= start && s.started_at <= now &&
                s.status == crate::FocusStatus::Completed).count(),
            focus_abandoned: self.state.focus_sessions.iter().filter(|s|
                s.started_at >= start && s.started_at <= now &&
                s.status == crate::FocusStatus::Abandoned).count(),
            focus_elapsed_minutes: self.state.focus_sessions.iter()
                .filter(|s| s.started_at >= start && s.started_at <= now)
                .filter_map(|s| s.ended_at.map(|end|
                    (end.saturating_sub(s.started_at) / 60).min(u64::from(s.target_minutes))))
                .sum(),
        };
        if n != 0 {
            summary.mean_fascination = recent.iter()
                .map(|c| f64::from(c.fascination)).sum::<f64>() / n as f64;
            summary.mean_wellbeing = recent.iter()
                .map(|c| f64::from(c.wellbeing)).sum::<f64>() / n as f64;
        }
        summary
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
            "homelab-conditioning-test-{}-{}", std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&dir).unwrap();
        (Engine::open(dir.join("state.json")).unwrap(), dir)
    }
    fn activate(e: &mut Engine) {
        e.update_profile(ProfileChange {
            expected_revision: 0, enabled: true, proactive_enabled: true,
            cadence_minutes: 120, holdout_weekday_utc: None, goal_id: "research".into(),
            identity: "Original researcher".into(),
            outcome: "Prove a useful theorem".into(),
            next_step: "Establish a lemma".into(),
            intensity: Intensity::Direct, daily_limit: 4,
            quiet_start_utc_hour: 22, quiet_end_utc_hour: 6,
        }).unwrap();
    }
    #[test]
    fn schedule_respects_explicit_consent_quiet_hours_and_pending_state() {
        let (mut e, dir) = engine();
        assert!(e.tick(86400 + 12 * 3600).unwrap().is_none());
        activate(&mut e);
        assert!(e.tick(86400 + 23 * 3600).unwrap().is_none());
        let one = e.tick(86400 + 12 * 3600).unwrap().unwrap();
        assert_eq!(e.snapshot().interventions.len(), 1);
        assert!(e.tick(86400 + 13 * 3600).unwrap().is_none());
        e.dismiss(one.id).unwrap();
        assert!(e.tick(86400 + 13 * 3600).unwrap().is_none());
        let second = e.tick(86400 + 16 * 3600).unwrap().unwrap();
        assert_ne!(one.id, second.id);
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn voluntary_work_can_be_measured_on_intervention_free_day() {
        let (mut e, dir) = engine();
        activate(&mut e);
        e.update_profile(ProfileChange {
            expected_revision: 1, enabled: true, proactive_enabled: true,
            cadence_minutes: 120, holdout_weekday_utc: Some(5),
            goal_id: "research".into(), identity: "Original researcher".into(),
            outcome: "Prove a useful theorem".into(),
            next_step: "Establish a lemma".into(), intensity: Intensity::Direct,
            daily_limit: 4, quiet_start_utc_hour: 22, quiet_end_utc_hour: 6,
        }).unwrap();
        let friday = 86400 + 12 * 3600;
        assert!(e.tick(friday).unwrap().is_none());
        assert!(matches!(e.suggest(friday), Err(Error::Limited(_))));
        e.checkin(CheckinInput {
            minutes: 50, self_initiated: true, fascination: 4, wellbeing: 4,
            intervention_id: None, evidence_ref: String::new(),
        }, friday).unwrap();
        let summary=e.metrics(friday + 10);
        assert_eq!(summary.holdout_minutes, 50);
        assert_eq!(summary.holdout_self_initiated_minutes, 50);
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn measured_fascination_and_independent_choice_survive_restart() {
        let (mut e, dir) = engine();
        activate(&mut e);
        let result = e.checkin(CheckinInput {
            minutes: 90, self_initiated: true, fascination: 5,
            wellbeing: 4, intervention_id: None, evidence_ref: "research:lemma-1".into(),
        }, 200000).unwrap();
        assert_eq!(result.checkins.len(), 1);
        let m = e.metrics(200001);
        assert_eq!(m.minutes, 90);
        assert_eq!(m.self_initiated_minutes, 90);
        assert_eq!(m.mean_fascination, 5.0);
        drop(e);
        let reopened=Engine::open(dir.join("state.json")).unwrap();
        assert_eq!(reopened.metrics(200001).sessions, 1);
        drop(reopened);
        fs::remove_dir_all(dir).unwrap();
    }
    #[test]
    fn adverse_wellbeing_pauses_scheduler_without_erasing_outcomes() {
        let (mut e, dir) = engine();
        activate(&mut e);
        e.checkin(CheckinInput {
            minutes: 25, self_initiated: false, fascination: 1,
            wellbeing: 0, intervention_id: None, evidence_ref: String::new(),
        }, 200000).unwrap();
        assert!(!e.snapshot().profile.enabled);
        assert!(!e.snapshot().profile.proactive_enabled);
        assert!(e.tick(240000).unwrap().is_none());
        drop(e);
        fs::remove_dir_all(dir).unwrap();
    }
}
