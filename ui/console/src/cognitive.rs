use dioxus::prelude::*;
use gloo_timers::future::TimeoutFuture;
use reqwest::Method;
use serde_json::{Value, json};

use crate::api;
use crate::components::{Card, Empty, Heading, Loading, Notice};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Phase {
    Idle,
    Pause,
    Waiting,
    Signal,
    Done,
}

fn milliseconds() -> Option<f64> {
    web_sys::window()?.performance().map(|clock| clock.now())
}

fn reaction_summary(trials: &[f64]) -> Option<(f64, f64)> {
    if trials.len() != 5 || trials.iter().any(|value| {
        !value.is_finite() || !(80.0..=1500.0).contains(value)
    }) {
        return None;
    }
    let mut sorted = trials.to_vec();
    sorted.sort_by(f64::total_cmp);
    Some((sorted[2], trials.iter().sum::<f64>() / trials.len() as f64))
}

// Each timer owns an epoch. Starting or restarting a trial invalidates
// callbacks from the earlier attempt before they can display a stimulus.
fn schedule_trial(
    mut phase: Signal<Phase>,
    mut epoch: Signal<u64>,
    mut stimulus: Signal<f64>,
    mut notice: Signal<String>,
    pause_ms: u32,
) {
    let token = epoch().wrapping_add(1);
    epoch.set(token);
    phase.set(Phase::Pause);
    spawn(async move {
        TimeoutFuture::new(pause_ms).await;
        if epoch() != token {
            return;
        }
        phase.set(Phase::Waiting);
        let delay = 1000 + (js_sys::Math::random() * 2500.0) as u32;
        TimeoutFuture::new(delay).await;
        if epoch() != token || phase() != Phase::Waiting {
            return;
        }
        if let Some(now) = milliseconds() {
            stimulus.set(now);
            phase.set(Phase::Signal);
            notice.set("Respond as soon as the target changes color.".to_owned());
        } else {
            phase.set(Phase::Idle);
            notice.set("The browser's high-resolution clock is unavailable.".to_owned());
        }
    });
}

#[component]
pub fn ReactionTime(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut phase = use_signal(|| Phase::Idle);
    let mut epoch = use_signal(|| 0_u64);
    let mut stimulus = use_signal(|| 0.0_f64);
    let mut trials = use_signal(Vec::<f64>::new);
    let mut saving = use_signal(|| false);
    let mut saved = use_signal(|| false);
    let mut notice = use_signal(String::new);
    let mut error = use_signal(String::new);
    let mut history = use_resource(|| async {
        api::get::<Value>("/v1/health/records").await
    });

    let progress = trials().len();
    let result = reaction_summary(&trials());
    let label = match phase() {
        Phase::Idle => "Start to begin".to_owned(),
        Phase::Pause => "Preparing the next trial".to_owned(),
        Phase::Waiting => "Wait for green".to_owned(),
        Phase::Signal => "Respond now".to_owned(),
        Phase::Done => "Five trials complete".to_owned(),
    };
    let class = match phase() {
        Phase::Signal => "reaction-target reaction-go",
        Phase::Waiting => "reaction-target reaction-ready",
        _ => "reaction-target",
    };

    rsx! {
        Heading {
            title: "Visual reaction time".to_owned(),
            description: "Five browser-timed responses, recorded as exploratory observations.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| history.restart(),
                "Refresh history"
            }
        }
        if !error().is_empty() {
            Notice { message: error() }
        }
        Card {
            title: "Response task".to_owned(),
            description: "Wait for the target to turn green, then respond immediately. Early and out-of-range responses are not counted.".to_owned(),
            div { class: "reaction-test",
                button {
                    class: class,
                    r#type: "button",
                    disabled: saving(),
                    aria_label: "Visual reaction target",
                    onclick: move |_| {
                        match phase() {
                            Phase::Idle | Phase::Done => {
                                trials.set(Vec::new());
                                saved.set(false);
                                error.set(String::new());
                                notice.set("Waiting for the first signal.".to_owned());
                                schedule_trial(phase, epoch, stimulus, notice, 0);
                            }
                            Phase::Waiting => {
                                notice.set("Too soon. Repeating this trial.".to_owned());
                                schedule_trial(phase, epoch, stimulus, notice, 800);
                            }
                            Phase::Signal => {
                                let elapsed = milliseconds().map(|now| now - stimulus()).unwrap_or(-1.0);
                                if !(80.0..=1500.0).contains(&elapsed) || !elapsed.is_finite() {
                                    notice.set("Invalid response time. Repeating this trial.".to_owned());
                                    schedule_trial(phase, epoch, stimulus, notice, 800);
                                } else {
                                    trials.write().push(elapsed);
                                    if trials().len() == 5 {
                                        phase.set(Phase::Done);
                                        notice.set("Results are ready. Review them before saving.".to_owned());
                                    } else {
                                        notice.set(format!("Recorded {elapsed:.0} ms."));
                                        schedule_trial(phase, epoch, stimulus, notice, 700);
                                    }
                                }
                            }
                            Phase::Pause => {}
                        }
                    },
                    "{label}"
                }
                div { class: "reaction-controls",
                    button {
                        class: "btn btn-outline",
                        r#type: "button",
                        disabled: saving(),
                        onclick: move |_| {
                            trials.set(Vec::new());
                            saved.set(false);
                            error.set(String::new());
                            notice.set("Waiting for the first signal.".to_owned());
                            schedule_trial(phase, epoch, stimulus, notice, 0);
                        },
                        "Restart five trials"
                    }
                    button {
                        class: "btn btn-primary",
                        r#type: "button",
                        disabled: saving() || saved() || phase() != Phase::Done || result.is_none(),
                        onclick: move |_| {
                            let Some((_, _)) = reaction_summary(&trials()) else {
                                error.set("Complete five valid trials before saving.".to_owned());
                                return;
                            };
                            let token = csrf();
                            let payload = json!({"trials_ms": trials()});
                            saving.set(true);
                            error.set(String::new());
                            spawn(async move {
                                match api::write::<Value, _>(
                                    Method::POST, "/v1/health/reaction-test", &token, &payload,
                                ).await {
                                    Ok(_) => {
                                        saved.set(true);
                                        notice.set("The five trials and their median were saved.".to_owned());
                                        history.restart();
                                    }
                                    Err(message) => error.set(message),
                                }
                                saving.set(false);
                            });
                        },
                        if saving() { "Saving…" } else if saved() { "Saved" } else { "Save results" }
                    }
                }
                div { class: "reaction-progress",
                    div {
                        span { class: "small muted", "Trials" }
                        strong { "{progress} / 5" }
                    }
                    div {
                        span { class: "small muted", "Median response" }
                        strong {
                            if let Some((median, _)) = result {
                                "{median:.1} ms"
                            } else {
                                "—"
                            }
                        }
                    }
                    div {
                        span { class: "small muted", "Mean response" }
                        strong {
                            if let Some((_, mean)) = result {
                                "{mean:.1} ms"
                            } else {
                                "—"
                            }
                        }
                    }
                }
                p { class: "small muted", role: "status", aria_live: "polite", "{notice()}" }
                p { class: "small muted",
                    "Display refresh, browser scheduling, and input hardware affect timing. This is not a clinical assessment."
                }
            }
        }
        Card {
            title: "Recorded responses".to_owned(),
            description: "Recent saved median response times, with their original measurement method.".to_owned(),
            match history.cloned() {
                None => rsx! { Loading {} },
                Some(Err(message)) => rsx! { Notice { message } },
                Some(Ok(value)) => {
                    let records = api::list(&value, &["records", "observations", "items"])
                        .into_iter()
                        .filter(|record| {
                            record.get("domain").and_then(Value::as_str) == Some("cognition")
                                && record.get("metric").and_then(Value::as_str) == Some("simple_reaction_ms")
                        })
                        .take(10)
                        .collect::<Vec<_>>();
                    rsx! {
                        if records.is_empty() {
                            Empty {
                                title: "No saved responses".to_owned(),
                                description: "Complete and save the test to add its results to Health.".to_owned()
                            }
                        } else {
                            div { class: "table-clip",
                                table { class: "table",
                                    thead { tr {
                                        th { "Recorded at" }
                                        th { "Median" }
                                        th { "Method" }
                                        th { "Source" }
                                    } }
                                    tbody {
                                        for item in records {
                                            tr {
                                                td { {api::property(&item, &["at"])} }
                                                td { {format!("{} ms", api::property(&item, &["value"]))} }
                                                td { {api::property(&item, &["method"])} }
                                                td { {api::property(&item, &["source"])} }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn median_and_mean_are_computed_from_five_valid_trials() {
        assert_eq!(reaction_summary(&[242.0, 210.0, 238.0, 270.0, 225.0]), Some((238.0, 237.0)));
        assert_eq!(reaction_summary(&[80.0, 1500.0, 120.0, 100.0, 90.0]), Some((100.0, 378.0)));
    }

    #[test]
    fn incomplete_or_invalid_sessions_have_no_summary() {
        assert!(reaction_summary(&[]).is_none());
        assert!(reaction_summary(&[200.0; 4]).is_none());
        assert!(reaction_summary(&[200.0, f64::NAN, 250.0, 260.0, 270.0]).is_none());
        assert!(reaction_summary(&[0.0, 200.0, 210.0, 220.0, 230.0]).is_none());
    }
}
