//! Read-only evidence views. Calibration, source review, and formal
//! entailment are separate from demonstrated learner mastery.
use dioxus::prelude::*;
use serde_json::{json, Value};

use crate::components::{Card, Notice};
use crate::embedded_learning_graph::LearningGraph;
use crate::mcp_app;

fn id_valid(value: &str) -> bool {
    !value.is_empty() && value.len() <= 128
        && value.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b,b'_'|b'-'))
}

fn as_text(value: &Value, keys: &[&str]) -> String {
    for key in keys {
        if let Some(text) = value.get(key).and_then(Value::as_str) {
            if !text.is_empty() { return text.to_owned(); }
        }
        if let Some(number) = value.get(key).and_then(Value::as_i64) {
            return number.to_string();
        }
    }
    "Not reported".to_owned()
}
fn pct(value: Option<&Value>) -> String {
    value.and_then(Value::as_f64)
        .filter(|n| n.is_finite() && (0.0..=1.0).contains(n))
        .map(|n| format!("{:.1}%", n * 100.0))
        .unwrap_or_else(|| "Not reported".to_owned())
}
fn calibration_view(value: &Value, name: &str) -> Option<Value> {
    value.get(name).filter(|row| row.is_object()).cloned()
}

pub fn reasoning_graph(value: &Value) -> Value {
    if value.get("propositions").is_none() { return value.clone(); }
    let nodes = value.get("propositions").and_then(Value::as_array)
        .into_iter().flatten().take(200).map(|p| {
            json!({
                "id":p.get("id").and_then(Value::as_str).unwrap_or(""),
                "title":p.get("statement").and_then(Value::as_str).unwrap_or("Proposition"),
                "status":p.get("status").and_then(Value::as_str).unwrap_or("unreviewed"),
                "summary":p.get("context").and_then(Value::as_str).unwrap_or(""),
            })
        }).collect::<Vec<_>>();
    let mut edges = Vec::new();
    if let Some(chains) = value.get("chains").and_then(Value::as_array) {
        for chain in chains.iter().take(200) {
            if let (Some(premises),Some(conclusion))=(
                chain.get("premises").and_then(Value::as_array),
                chain.get("conclusion").and_then(Value::as_str)) {
                for premise in premises.iter().take(24).filter_map(Value::as_str) {
                    edges.push(json!({"source":premise,"target":conclusion,
                        "kind":chain.get("relation").and_then(Value::as_str)
                            .unwrap_or("requires")}));
                }
            }
        }
    }
    if let Some(disputes)=value.get("disputes").and_then(Value::as_array) {
        for item in disputes.iter().take(100) {
            edges.push(json!({
                "source":item.get("left_id"),"target":item.get("right_id"),
                "kind":"dispute"
            }));
        }
    }
    json!({"nodes":nodes,"edges":edges})
}

#[component]
pub fn CalibrationPanel(on_question:EventHandler<String>,on_problem:EventHandler<String>)->Element {
    let mut result=use_signal(||None::<Value>);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    rsx! {
        Card {
            title:"Confidence calibration".to_owned(),
            description:"Compare self-reported certainty against checked outcomes; descriptive statistics never award mastery.".to_owned(),
            div{class:"stack",
                if !error().is_empty() {Notice{message:error()}}
                button{class:"btn btn-outline",r#type:"button",disabled:busy(),
                    onclick:move |_|{
                        if busy(){return}
                        busy.set(true);error.set(String::new());
                        spawn(async move{
                            match mcp_app::call_learning_tool::<Value>(
                                "get_confidence_calibration",json!({"limit":250})).await {
                                Ok(value)=>result.set(Some(value)),
                                Err(message)=>error.set(message),
                            }
                            busy.set(false);
                        });
                    },
                    if busy() {"Loading calibration…"}else{"Load calibration evidence"}
                }
                if let Some(report)=result(){
                    for (title,key,kind) in [
                        ("Precommitted multiple-choice answers","choice_accuracy","question"),
                        ("Authenticated checked results","authenticated_result_accuracy","problem")
                    ] {
                        if let Some(cohort)=calibration_view(&report,key) {
                            CalibrationCohort {
                                name:title.to_owned(),
                                evidence:cohort,
                                kind:kind.to_owned(),
                                on_question:on_question.clone(),
                                on_problem:on_problem.clone()
                            }
                        }
                    }
                    p{class:"small muted",
                        {as_text(&report,&["limitations"])}
                    }
                }
            }
        }
    }
}

#[component]
fn CalibrationCohort(name:String,evidence:Value,kind:String,
    on_question:EventHandler<String>,on_problem:EventHandler<String>)->Element {
    let samples=evidence.get("sample_count").and_then(Value::as_u64).unwrap_or(0);
    rsx! {
        section{class:"stack",
            h3{"{name}"}
            p{class:"small muted",
                "{samples} rated responses · "
                {as_text(&evidence,&["eligible_examined"])}
                " eligible · "
                {as_text(&evidence,&["missing_self_confidence"])}
                " missing confidence ratings"
            }
            if samples>0 {
                p{
                    "Mean certainty: " {pct(evidence.get("mean_confidence"))}
                    " · accuracy: " {pct(evidence.get("observed_accuracy"))}
                    " · Brier score: "
                    {evidence.get("brier_score").and_then(Value::as_f64)
                        .map(|x|format!("{x:.3}")).unwrap_or_else(||"N/A".to_owned())}
                }
                if evidence.get("insufficient_samples").and_then(Value::as_bool)==Some(true){
                    p{class:"small muted",
                        "Fewer than ten observations; this estimate is too sparse for a dependable conclusion."
                    }
                }
                if evidence.get("conflicting_checks_excluded").and_then(Value::as_u64).unwrap_or(0)>0{
                    p{class:"small muted",
                        "Conflicting authenticated results excluded: "
                        {as_text(&evidence,&["conflicting_checks_excluded"])}
                    }
                }
                if let Some(bands)=evidence.get("bands").and_then(Value::as_array){
                    div{class:"task-items",
                        for (index,band) in bands.iter().enumerate(){
                            if band.get("count").and_then(Value::as_u64).unwrap_or(0)>0 {
                                p{key:"band-{index}",class:"small muted",
                                    {as_text(band,&["band"])} " · "
                                    {as_text(band,&["count"])} " responses · certainty "
                                    {pct(band.get("mean_confidence"))}
                                    " · accuracy " {pct(band.get("accuracy"))}
                                }
                            }
                        }
                    }
                }
                for (label,key) in [
                    ("Confident errors","confident_errors"),
                    ("Underconfident successes","underconfident_successes")
                ] {
                    if let Some(examples)=evidence.get(key).and_then(Value::as_array){
                        if !examples.is_empty(){
                            h4{"{label}"}
                            div{class:"task-items",
                                for (index,example) in examples.iter().enumerate(){
                                    if let Some(id)=example.get("id").and_then(Value::as_str) {
                                        if id_valid(id){
                                            button{key:"example-{key}-{index}",class:"btn btn-outline btn-sm",
                                                r#type:"button",
                                                onclick:{
                                                    let id=id.to_owned();
                                                    let kind=kind.clone();
                                                    let on_question=on_question.clone();
                                                    let on_problem=on_problem.clone();
                                                    move |_|{
                                                        if kind=="question" {on_question.call(id.clone())}
                                                        else {on_problem.call(id.clone())}
                                                    }
                                                },
                                                {as_text(example,&["domain"])}
                                                " · certainty "
                                                {pct(example.get("self_confidence"))}
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }else{
                p{class:"small muted",
                    "No independently checkable outcome has an accompanying confidence estimate."
                }
            }
        }
    }
}

#[component]
pub fn ConceptEvidence(id:String)->Element{
    let mut concept=use_signal(||None::<Value>);
    let mut portfolio=use_signal(||None::<Value>);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    rsx!{
        Card{
            title:"Concept evidence".to_owned(),
            description:"Inspect prerequisites, independent assessments, and retention without changing mastery.".to_owned(),
            div{class:"stack",
                if !error().is_empty(){Notice{message:error()}}
                button{class:"btn btn-outline",r#type:"button",
                    disabled:busy()||!id_valid(&id),
                    onclick:{
                        let selected=id.clone();
                        move |_|{
                            if busy()||!id_valid(&selected){return}
                            let identifier=selected.clone();
                            busy.set(true);error.set(String::new());
                            spawn(async move {
                                let summary=mcp_app::call_learning_tool::<Value>(
                                    "get_concept",json!({"identifier":identifier})).await;
                                match summary{
                                    Ok(value)=>concept.set(Some(value)),
                                    Err(message)=>error.set(message),
                                }
                                busy.set(false);
                            });
                        }
                    },
                    "Inspect selected concept"
                }
                button{class:"btn btn-outline",r#type:"button",
                    disabled:busy()||!id_valid(&id),
                    onclick:{
                        let selected=id.clone();
                        move |_|{
                            if busy()||!id_valid(&selected){return}
                            busy.set(true);error.set(String::new());
                            let concept_id=selected.clone();
                            spawn(async move{
                                match mcp_app::call_learning_tool::<Value>(
                                    "get_mastery_portfolio",json!({"concept_id":concept_id})).await{
                                    Ok(value)=>portfolio.set(Some(value)),
                                    Err(message)=>error.set(message),
                                }
                                busy.set(false);
                            });
                        }
                    },
                    "Inspect mastery portfolio"
                }
                if let Some(value)=concept() {
                    h3{ {as_text(&value,&["title","id"])} }
                    p{class:"small muted", {as_text(&value,&["summary","explanation"])} }
                    pre{class:"mono small","{serde_json::to_string_pretty(&value).unwrap_or_default()}"}
                }
                if let Some(value)=portfolio(){
                    h3{"Evidence portfolio"}
                    pre{class:"mono small","{serde_json::to_string_pretty(&value).unwrap_or_default()}"}
                }
            }
        }
    }
}

#[component]
pub fn ReasoningEvidence(graph:Value,on_select:EventHandler<String>)->Element{
    let mut selection=use_signal(String::new);
    let mut result=use_signal(||None::<Value>);
    let mut history=use_signal(||None::<Value>);
    let mut translation=use_signal(||None::<Value>);
    let mut error=use_signal(String::new);
    let mut busy=use_signal(||false);
    let network=reasoning_graph(&graph);
    let chains=graph.get("chains").and_then(Value::as_array).cloned().unwrap_or_default();
    rsx!{
        Card{
            title:"Source-grounded reasoning".to_owned(),
            description:"Premises must jointly support a conclusion. A reviewed translation is not a certified natural-language proof.".to_owned(),
            div{class:"stack",
                if !error().is_empty(){Notice{message:error()}}
                LearningGraph {data:network,on_select:move |id:String|{
                    selection.set(id.clone());on_select.call(id);
                    result.set(None);history.set(None);translation.set(None);
                }}
                label{class:"form-row",
                    span{class:"label","Proposition, chain, or formal-check identifier"}
                    input{class:"field",maxlength:"128",value:"{selection()}",
                        oninput:move |event|selection.set(event.value())}
                }
                div{class:"inline-row",
                    for (title,tool,key) in [
                        ("Proposition","get_proposition","proposition_id"),
                        ("Reasoning chain","get_reasoning_chain","chain_id"),
                        ("Formal checks","get_reasoning_formal_checks","chain_id"),
                        ("Translation reviews","get_translation_reviews","check_id")
                    ] {
                        button{class:"btn btn-outline btn-sm",r#type:"button",
                            disabled:busy()||!id_valid(&selection()),
                            onclick:move |_|{
                                if busy()||!id_valid(&selection()){return}
                                let id=selection();
                                let request=json!({key:id});
                                busy.set(true);error.set(String::new());
                                spawn(async move{
                                    match mcp_app::call_learning_tool::<Value>(tool,request).await{
                                        Ok(value)=>{
                                            if tool=="get_translation_reviews"{translation.set(Some(value))}
                                            else if tool=="get_reasoning_formal_checks"{history.set(Some(value))}
                                            else{result.set(Some(value))}
                                        }
                                        Err(message)=>error.set(message),
                                    }
                                    busy.set(false);
                                });
                            },
                            "{title}"
                        }
                    }
                }
                if let Some(value)=result(){
                    h3{"Claim and provenance"}
                    pre{class:"mono small","{serde_json::to_string_pretty(&value).unwrap_or_default()}"}
                }
                if let Some(value)=history(){
                    h3{"Propositional checks"}
                    pre{class:"mono small","{serde_json::to_string_pretty(&value).unwrap_or_default()}"}
                }
                if let Some(value)=translation(){
                    h3{"Source-anchored translation judgments"}
                    pre{class:"mono small","{serde_json::to_string_pretty(&value).unwrap_or_default()}"}
                }
                if !chains.is_empty(){
                    h3{"Premise chains"}
                    for (index,chain) in chains.iter().enumerate(){
                        div{key:"chain-{index}",class:"task-item",
                            strong{ {as_text(chain,&["relation","status"])} }
                            p{class:"small muted",
                                "Conclusion: " {as_text(chain,&["conclusion"])}
                                " · " {as_text(chain,&["status"])}
                            }
                            p{class:"small muted", {as_text(chain,&["explanation"])} }
                            if let Some(premises)=chain.get("premises").and_then(Value::as_array){
                                p{class:"small muted",
                                    "Joint premises: "
                                    {premises.iter().filter_map(Value::as_str)
                                        .collect::<Vec<_>>().join(" AND ")}
                                }
                            }
                            if chain.get("review_active").and_then(Value::as_bool)!=Some(true){
                                p{class:"small muted",
                                    "Chain review is absent or its prerequisites are stale."
                                }
                            }
                            if let Some(id)=chain.get("id").and_then(Value::as_str){
                                if id_valid(id){
                                    button{class:"btn btn-outline btn-sm",r#type:"button",
                                        onclick:{
                                            let id=id.to_owned();
                                            move |_|selection.set(id.clone())
                                        },"Select chain"}
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
mod tests{
    use super::*;
    #[test]
    fn calibration_reports_do_not_create_mastery(){
        assert_eq!(pct(Some(&json!(0.6))),"60.0%");
        assert_eq!(pct(Some(&json!(1.2))),"Not reported");
        assert_eq!(calibration_view(&json!({}),"choice_accuracy"),None);
    }
    #[test]
    fn reasoning_links_only_use_stored_premises(){
        let record=json!({
            "propositions":[{"id":"p_a","statement":"Premise"},{"id":"p_b","statement":"Conclusion"}],
            "chains":[{"id":"c1","premises":["p_a"],"conclusion":"p_b","relation":"implies"}]
        });
        let projected=reasoning_graph(&record);
        assert_eq!(projected["nodes"].as_array().unwrap().len(),2);
        assert_eq!(projected["edges"][0]["source"],"p_a");
        assert_eq!(projected["edges"][0]["target"],"p_b");
    }
}
