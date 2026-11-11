//! Host-mediated Learning workspace. Browser code cannot grade, modify
//! prerequisite evidence, or obtain an independent security credential.
use dioxus::prelude::*;
use serde_json::{json, Value};
use crate::components::{Card, Empty, Heading, Notice};
use crate::mcp_app;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Panel { Graph, Sessions, Question, Problems, Sources, Reviews, Practice, Reasoning }

impl Panel {
    fn label(self) -> &'static str {
        match self {
            Self::Graph => "Knowledge graph",
            Self::Sessions => "Learning sessions",
            Self::Question => "Question",
            Self::Problems => "Problems",
            Self::Sources => "Sources",
            Self::Reviews => "Review queue",
            Self::Practice => "Practice",
            Self::Reasoning => "Reasoning",
        }
    }

    fn from_view(view:&str)->Option<Self>{
        match view{
            "graph"=>Some(Self::Graph),
            "session"=>Some(Self::Sessions),
            "learning_question"|"question"=>Some(Self::Question),
            "problem"=>Some(Self::Problems),
            "sources"=>Some(Self::Sources),
            "practice"=>Some(Self::Practice),
            "reasoning"=>Some(Self::Reasoning),
            _=>None,
        }
    }

    fn read(self, id: &str) -> Result<(&'static str,Value),String> {
        let valid = !id.is_empty() && id.len() <= 128 &&
            id.bytes().all(|byte| byte.is_ascii_alphanumeric()
                || byte == b'-' || byte == b'_');
        match self {
            Self::Graph => Ok(("get_graph",json!({"focus":if valid {id}else{""},
                "depth":2,"limit":80}))),
            Self::Sessions if valid => Ok(("get_learning_session",json!({"session_id":id}))),
            Self::Sessions => Ok(("list_learning_sessions",json!({"limit":50}))),
            Self::Question if valid => Ok(("get_learning_question",json!({"question_id":id}))),
            Self::Question => Err("Select a question identifier.".to_owned()),
            Self::Problems if valid => Ok(("get_problem",json!({"problem_id":id}))),
            Self::Problems => Ok(("list_problems",json!({"limit":50}))),
            Self::Sources => Ok(("list_learning_sources",json!({"limit":80}))),
            Self::Reviews => Ok(("get_review_queue",json!({"limit":80}))),
            Self::Practice if valid => Ok(("get_practice_session",json!({"session_id":id}))),
            Self::Practice => Ok(("list_practice_sessions",json!({"limit":50}))),
            Self::Reasoning if valid => Ok(("get_reasoning_chain",json!({"chain_id":id}))),
            Self::Reasoning => Ok(("get_reasoning_graph",json!({"limit":80}))),
        }
    }
}

fn list_records(value:&Value)->Vec<Value>{
    if let Some(items)=value.as_array(){return items.clone()}
    for field in ["concepts","nodes","sessions","questions","problems","sources",
        "queue","reviews","items","chains","propositions","steps","attempts"] {
        if let Some(items)=value.get(field).and_then(Value::as_array){
            return items.clone()
        }
    }
    Vec::new()
}
fn field(value:&Value, names:&[&str])->String{
    for name in names{
        if let Some(text)=value.get(name).and_then(Value::as_str){
            if !text.is_empty(){return text.to_owned()}
        }
        if let Some(n)=value.get(name).and_then(Value::as_i64){return n.to_string()}
    }
    "—".to_owned()
}
fn valid_identity(s:&str)->bool{
    !s.is_empty()&&s.len()<=128&&s.bytes().all(|byte|
        byte.is_ascii_alphanumeric()||byte==b'-'||byte==b'_')
}

#[component]
pub fn LearningApp() -> Element {
    let mut panel=use_signal(||Panel::Graph);
    let mut selected_id=use_signal(String::new);
    let mut result=use_signal(||None::<Value>);
    let mut loading=use_signal(||false);
    let mut error=use_signal(String::new);
    let mut notice=use_signal(String::new);
    let mut target_level=use_signal(||3_i64);
    let mut target_busy=use_signal(||false);
    rsx!{
        Heading {
            title:"Learning workspace".to_owned(),
            description:"Source-grounded reasoning, problems and independent assessments.".to_owned(),
            div {}
        }
        div { class:"health-plan-list",role:"group",aria_label:"Learning sections",
            for section in [Panel::Graph,Panel::Sessions,Panel::Question,
                Panel::Problems,Panel::Sources,Panel::Reviews,Panel::Practice,Panel::Reasoning] {
                button {
                    class:if panel()==section{"btn btn-primary"}else{"btn btn-outline"},
                    aria_pressed:panel()==section,
                    onclick:move |_|{
                        panel.set(section);result.set(None);error.set(String::new());
                    },
                    "{section.label()}"
                }
            }
        }
        if panel()==Panel::Graph {
            div{class:"inline-row",
                label{class:"form-row",
                    span{class:"label","Verified mastery target"}
                    select{class:"field",disabled:target_busy(),
                        value:"{target_level()}",
                        onchange:move |event|{
                            if let Ok(level)=event.value().parse::<i64>(){
                                if (1..=5).contains(&level){target_level.set(level)}
                            }
                        },
                        for level in 1..=5 {
                            option{value:"{level}","Level {level}"}
                        }
                    }
                }
                button{class:"btn btn-outline",r#type:"button",
                    disabled:target_busy(),
                    onclick:move |_|{
                        let level=target_level();
                        if target_busy()||!(1..=5).contains(&level){return}
                        target_busy.set(true);error.set(String::new());
                        spawn(async move{
                            match mcp_app::call_learning_tool::<Value>(
                                "set_learning_target",json!({"level":level})).await{
                                Ok(_)=>{
                                    result.set(None);
                                    notice.set("Mastery target saved; earlier evidence remains unchanged.".to_owned());
                                }
                                Err(message)=>error.set(message),
                            }
                            target_busy.set(false);
                        });
                    },
                    "Save mastery target"
                }
            }
        }
        button{
            class:"btn btn-outline",r#type:"button",
            onclick:move |_|{
                match mcp_app::take_host_learning_result(){
                    Some(value)=>{
                        if let Some(view)=value.get("view").and_then(Value::as_str)
                            .and_then(Panel::from_view){
                            panel.set(view);
                        }
                        if let Some(id)=value.get("id").and_then(Value::as_str)
                            .filter(|id|valid_identity(id)){
                            selected_id.set(id.to_owned());
                        }
                        result.set(Some(value));
                        error.set(String::new());
                        notice.set("Host-provided Learning record loaded.".to_owned());
                    }
                    None=>notice.set("No host result is available. Use the Learning search controls.".to_owned()),
                }
            },
            "Open originating tool result"
        }
        Card {
            title:panel().label().to_owned(),
            description:"Tool results are supplied by the Learning MCP host. No browser credential is stored.".to_owned(),
            div { class:"card-body stack",
                if !error().is_empty(){ Notice { message:error() } }
                if !notice().is_empty(){p{class:"small muted",role:"status","{notice()}"}}
                label { class:"form-row",
                    span { class:"label","Record identifier or optional graph focus" }
                    input {
                        class:"field",maxlength:"128",value:"{selected_id()}",
                        placeholder:"Leave empty to browse the collection",
                        oninput:move |event|selected_id.set(event.value())
                    }
                }
                button {
                    class:"btn btn-outline",disabled:loading(),
                    onclick:move |_|{
                        if loading(){return}
                        let (tool,args)=match panel().read(&selected_id()){
                            Ok(value)=>value,
                            Err(message)=>{error.set(message);return}
                        };
                        loading.set(true);error.set(String::new());notice.set(String::new());
                        spawn(async move{
                            match mcp_app::call_learning_tool::<Value>(tool,args).await{
                                Ok(value)=>result.set(Some(value)),
                                Err(message)=>error.set(message),
                            }
                            loading.set(false);
                        });
                    },
                    if loading(){"Loading…"}else{"Load from Learning"}
                }
                if let Some(data)=result(){
                    if panel()==Panel::Graph {
                        crate::embedded_learning_graph::LearningGraph {
                            data:data.clone(),
                            on_select:move |id:String|{
                                selected_id.set(id);
                                result.set(None);
                            }
                        }
                    }
                    if list_records(&data).is_empty(){
                        pre { class:"mono small", "{serde_json::to_string_pretty(&data).unwrap_or_default()}" }
                    }else{
                        div { class:"task-items",
                            for (index,record) in list_records(&data).iter().enumerate(){
                                div { class:"task-item",key:"{index}",
                                    div { class:"task-information",
                                        strong { {field(record,&["title","label","name","goal","statement","prompt","id"])} }
                                        span { class:"small muted",
                                            {field(record,&["phase","kind","status","domain","due_at"])}
                                        }
                                        if let Some(id)=record.get("id").and_then(Value::as_str){
                                            button {
                                                class:"btn btn-outline btn-sm",r#type:"button",
                                                onclick:{
                                                    let id=id.to_owned();
                                                    move |_|selected_id.set(id.clone())
                                                },
                                                "Select"
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }else{
                    Empty {
                        title:"Choose Learning records".to_owned(),
                        description:"Open a graph, session, question, problem, source, review or practice record.".to_owned()
                    }
                }
            }
        }
        if panel()==Panel::Question {
            EmbeddedAnswer {
                question:result(),
                on_saved:move |_|{
                    result.set(None);
                    notice.set("The original response was saved. An independent assessment remains necessary.".to_owned());
                }
            }
        }
        if panel()==Panel::Problems {
            EmbeddedProblem {key:"{selected_id()}",problem:result()}
        }
        if panel()==Panel::Sessions {
            EmbeddedSession {key:"{selected_id()}",
                session:result(),
                session_id:selected_id(),
                on_saved:move |_|{result.set(None);notice.set("Learning session action accepted.".to_owned());}
            }
        }
        if panel()==Panel::Sources {
            EmbeddedSource { source_id:selected_id() }
        }
        if let Some(kind)=match panel(){
            Panel::Sessions=>Some("session"),
            Panel::Problems=>Some("problem"),
            Panel::Sources=>Some("source"),
            _=>None,
        } {
            if valid_identity(&selected_id()){
                crate::embedded_learning_attachments::EmbeddedAttachments{
                    owner_kind:kind.to_owned(),owner_id:selected_id()
                }
            }
        }
    }
}

#[component]
fn EmbeddedProblem(problem:Option<Value>)->Element {
    let mut draft=use_signal(String::new);
    let mut reasoning=use_signal(String::new);
    let mut confidence=use_signal(String::new);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    let mut receipt=use_signal(||None::<Value>);
    let Some(problem)=problem else{return rsx!{}};
    let shown=receipt().as_ref().and_then(|value|value.get("problem")).cloned()
        .unwrap_or(problem);
    let id=field(&shown,&["id"]);
    if !valid_identity(&id)||shown.get("statement").and_then(Value::as_str).is_none() {
        return rsx!{};
    }
    let open=shown.get("status").and_then(Value::as_str)==Some("open");
    let last=shown.get("drafts").and_then(Value::as_array)
        .and_then(|drafts|drafts.last()).cloned();
    rsx!{
        Card {
            title:field(&shown,&["title"]),
            description:"Independent problem drafts are saved as immutable revisions.".to_owned(),
            div{class:"card-body stack",
                p{{field(&shown,&["statement"])}}
                p{class:"small muted",
                    "Status: " {field(&shown,&["status"])}
                    " · Mode: " {field(&shown,&["mode"])}
                }
                if !error().is_empty(){Notice{message:error()}}
                if let Some(confirmation)=receipt(){
                    div{class:"alert",role:"status",
                        p{"Draft saved. No review, final verdict or mastery credit was recorded."}
                        p{class:"mono small","Revision: " {field(&confirmation,&["draft_id"])}}
                    }
                }
                if open {
                    if let Some(previous)=last {
                        button {class:"btn btn-outline",r#type:"button",disabled:busy(),
                            onclick:move |_|{
                                draft.set(previous.get("body").and_then(Value::as_str)
                                    .unwrap_or("").to_owned());
                                reasoning.set(previous.get("learner_reasoning")
                                    .and_then(Value::as_str).unwrap_or("").to_owned());
                                confidence.set(previous.get("self_confidence")
                                    .and_then(Value::as_f64).map(|x|x.to_string())
                                    .unwrap_or_default());
                                receipt.set(None);
                            },
                            "Continue last saved draft"
                        }
                    }
                    form{class:"task-editor-fields",
                        onsubmit:{
                            let id=id.clone();
                            move |event|{
                                event.prevent_default();
                                let body=draft();
                                let explanation=reasoning();
                                let confidence_text=confidence();
                                let estimate=if confidence_text.is_empty(){None}
                                    else{confidence_text.parse::<f64>().ok()};
                                if busy()||!valid_identity(&id)||body.trim().is_empty()
                                    ||body.len()>16000||explanation.len()>16000
                                    ||!confidence_text.is_empty()&&estimate.is_none(){
                                    error.set("Enter an independent solution and valid confidence.".to_owned());
                                    return;
                                }
                                let params=json!({"problem_id":id,"draft":body,
                                    "learner_reasoning":explanation,
                                    "self_confidence":estimate});
                                busy.set(true);error.set(String::new());receipt.set(None);
                                spawn(async move{
                                    match mcp_app::call_learning_tool::<Value>(
                                        "save_problem_draft",params).await{
                                        Ok(saved)=>receipt.set(Some(saved)),
                                        Err(message)=>error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        label{class:"form-row",
                            span{class:"label","My solution"}
                            textarea{class:"field",rows:"6",required:true,maxlength:"16000",
                                disabled:busy(),value:"{draft()}",
                                oninput:move |event|draft.set(event.value())}
                        }
                        label{class:"form-row",
                            span{class:"label","Independent reasoning (optional)"}
                            textarea{class:"field",rows:"4",maxlength:"16000",
                                disabled:busy(),value:"{reasoning()}",
                                oninput:move |event|reasoning.set(event.value())}
                        }
                        label{class:"form-row",
                            span{class:"label","Self-reported confidence"}
                            select{class:"field",disabled:busy(),value:"{confidence()}",
                                onchange:move |event|confidence.set(event.value()),
                                option{value:"","Not specified"}
                                option{value:"0","0%"}
                                option{value:"0.25","25%"}
                                option{value:"0.5","50%"}
                                option{value:"0.75","75%"}
                                option{value:"1","100%"}
                            }
                        }
                        button{class:"btn btn-primary",disabled:busy(),"Save independent draft"}
                    }
                }else{
                    p{class:"small muted","This problem is closed; saved evidence remains available for review."}
                }
            }
        }
    }
}

#[component]
fn EmbeddedAnswer(question: Option<Value>,on_saved:EventHandler<()>) -> Element {
    let mut model_consent=use_signal(||false);
    let mut model_proposal=use_signal(||None::<Value>);
    let mut answer=use_signal(String::new);
    let mut reasoning=use_signal(String::new);
    let mut confidence=use_signal(String::new);
    let mut assisted=use_signal(||false);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    let Some(question)=question else {return rsx!{}};
    let id=field(&question,&["id"]);
    let status=field(&question,&["status"]);
    let options=question.get("options").and_then(Value::as_array).cloned().unwrap_or_default();
    let multi=question.get("response_kind").and_then(Value::as_str)==Some("multiple_choice");
    rsx!{
        Card{
            title:"Answer the question".to_owned(),
            description:"A correct option without supported reasoning is not evidence of mastery.".to_owned(),
            div{class:"card-body stack",
                p{ {field(&question,&["prompt"])} }
                if status=="answered"{
                    div{class:"alert",role:"status","Response saved. Await independent assessment."}
                    p{ {field(&question,&["learner_response"])} }
                    p{class:"small muted",{field(&question,&["learner_reasoning"])}}
                    label{class:"inline-row",
                        input{r#type:"checkbox",checked:model_consent(),disabled:busy(),
                            onchange:move |event|model_consent.set(event.checked())}
                        span{"Disclose this submitted answer, reasoning and committed grading reference to the configured model for provisional assessment."}
                    }
                    button{
                        class:"btn btn-outline",r#type:"button",disabled:busy()||!model_consent(),
                        onclick:{
                            let question_id=id.clone();
                            move |_|{
                                if busy()||!model_consent()||!valid_identity(&question_id){return}
                                let args=json!({"question_id":question_id,
                                    "allow_model_disclosure":true});
                                error.set(String::new());model_proposal.set(None);busy.set(true);
                                spawn(async move{
                                    match mcp_app::call_learning_tool::<Value>(
                                        "propose_question_model_assessment",args).await{
                                        Ok(value)=>{model_proposal.set(Some(value));model_consent.set(false);}
                                        Err(message)=>error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        "Request provisional question feedback"
                    }
                    if let Some(value)=model_proposal(){
                        p{class:"small muted",
                            "This proposal is not a recorded grade and awards no mastery."
                        }
                        pre{class:"mono small",
                            "{serde_json::to_string_pretty(&value).unwrap_or_default()}"
                        }
                    }
                }else if status!="pending" {
                    p{class:"small muted","This question is not awaiting a learner answer."}
                }else{
                    if !error().is_empty(){Notice{message:error()}}
                    form{
                        class:"task-editor-fields",
                        onsubmit:{
                            let id=id.clone();
                            let options=options.clone();
                            move |event|{
                                event.prevent_default();
                                if busy() || !valid_identity(&id) || answer().trim().is_empty()||
                                    answer().len()>12000||reasoning().len()>16000||
                                    multi&&!options.iter().any(|o|
                                        o.get("id").and_then(Value::as_str)==Some(answer().as_str())){
                                    error.set("Choose a valid response and reasoning before submitting.".to_owned());
                                    return;
                                }
                                let answer_text=answer();
                                let reasoning_text=reasoning();
                                let certainty=confidence().parse::<f64>().ok();
                                let params=json!({"question_id":id,
                                    "learner_response":answer_text,"learner_reasoning":reasoning_text,
                                    "assisted":assisted(),"self_confidence":certainty});
                                busy.set(true);error.set(String::new());
                                spawn(async move{
                                    match mcp_app::call_learning_tool::<Value>(
                                        "answer_learning_question",params).await{
                                        Ok(_)=>on_saved.call(()),
                                        Err(message)=>error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        if multi{
                            label{class:"form-row",
                                span{class:"label","Answer" }
                                select{
                                    class:"field",required:true,disabled:busy(),value:"{answer()}",
                                    onchange:move |event|answer.set(event.value()),
                                    option{value:"","Select an option"}
                                    for option in &options{
                                        if let (Some(id),Some(title))=(
                                            option.get("id").and_then(Value::as_str),
                                            option.get("label").and_then(Value::as_str)){
                                            option{value:"{id}","{title}"}
                                        }
                                    }
                                }
                            }
                        }else{
                            label{class:"form-row",
                                span{class:"label","Answer"}
                                textarea{class:"field",rows:"3",maxlength:"12000",required:true,
                                    disabled:busy(),value:"{answer()}",
                                    oninput:move |event|answer.set(event.value())}
                            }
                        }
                        label{class:"form-row",
                            span{class:"label","Independent reasoning"}
                            textarea{class:"field",rows:"4",maxlength:"16000",
                                disabled:busy(),value:"{reasoning()}",
                                oninput:move |event|reasoning.set(event.value())}
                        }
                        label{class:"form-row",
                            span{class:"label","Confidence (optional)"}
                            select{class:"field",value:"{confidence()}",disabled:busy(),
                                onchange:move |event|confidence.set(event.value()),
                                option{value:"","No estimate"}
                                option{value:"0.0","0%"}
                                option{value:"0.25","25%"}
                                option{value:"0.5","50%"}
                                option{value:"0.75","75%"}
                                option{value:"1.0","100%"}
                            }
                        }
                        label{class:"inline-row",
                            input{r#type:"checkbox",checked:assisted(),disabled:busy(),
                                onchange:move |event|assisted.set(event.checked())}
                            span{"I used assistance"}
                        }
                        button{class:"btn btn-primary",disabled:busy(),"Record my answer"}
                    }
                }
            }
        }
    }
}

fn permitted_session_phases(phase:&str)->&'static [&'static str]{
    match phase {
        "diagnose"=>&["plan"],
        "plan"=>&["diagnose","teach"],
        "teach"=>&["plan","practice"],
        "practice"=>&["teach","reassess"],
        "reassess"=>&["diagnose","practice","complete"],
        _=>&[],
    }
}

#[component]
fn EmbeddedSession(session:Option<Value>,session_id:String,on_saved:EventHandler<()>) -> Element {
    let mut goal=use_signal(String::new);
    let mut concept=use_signal(String::new);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    let mut consent=use_signal(||false);
    let mut step_response=use_signal(String::new);
    let mut step_id=use_signal(String::new);
    let mut feedback=use_signal(||None::<Value>);
    let mut next_phase=use_signal(String::new);
    let phase=session.as_ref().and_then(|v|v.get("phase")).and_then(Value::as_str).unwrap_or("");
    let revision=session.as_ref().and_then(|v|v.get("revision")).and_then(Value::as_i64);
    let transitions=permitted_session_phases(phase);
    let can_plan=matches!(phase,"plan"|"teach"|"practice"|"reassess");
    let on_saved_build=on_saved.clone();
    let on_saved_advance=on_saved.clone();
    rsx!{
        Card{
            title:"Learning sessions and reasoning".to_owned(),
            description:"The host stores evidence. Model feedback is provisional and requires explicit disclosure consent.".to_owned(),
            div{class:"card-body stack",
                if !error().is_empty(){Notice{message:error()}}
                if let Some(value)=feedback(){
                    pre{class:"mono small","{serde_json::to_string_pretty(&value).unwrap_or_default()}"}
                }
                form{class:"task-editor-fields",
                    onsubmit:move |event|{
                        event.prevent_default();
                        if busy()||goal().trim().len()<2||!valid_identity(&concept()){return}
                        let params=json!({"goal":goal(),"target_concept_id":concept(),"mode":"learn"});
                        busy.set(true);error.set(String::new());
                        spawn(async move{
                            match mcp_app::call_learning_tool::<Value>("start_learning_session",params).await{
                                Ok(_)=>on_saved.call(()),
                                Err(msg)=>error.set(msg),
                            }
                            busy.set(false);
                        });
                    },
                    label{class:"form-row",
                        span{class:"label","Learning objective"}
                        input{class:"field",required:true,maxlength:"500",value:"{goal()}",
                            oninput:move |event|goal.set(event.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Existing concept ID"}
                        input{class:"field",required:true,maxlength:"128",value:"{concept()}",
                            oninput:move |event|concept.set(event.value())}
                    }
                    button{class:"btn btn-outline",disabled:busy(),"Start learning session"}
                }
                if let Some(session)=session{
                    if let Some(version)=revision{
                        if can_plan {
                            button {class:"btn btn-outline",r#type:"button",
                                disabled:busy()||!valid_identity(&session_id),
                                onclick:{
                                    let id=session_id.clone();
                                    let on_saved=on_saved_build.clone();
                                    move |_|{
                                        if busy()||!valid_identity(&id){return}
                                        let args=json!({"session_id":id,
                                            "expected_revision":version});
                                        busy.set(true);error.set(String::new());
                                        spawn(async move{
                                            match mcp_app::call_learning_tool::<Value>(
                                                "build_learning_plan",args).await {
                                                Ok(_)=>on_saved.call(()),
                                                Err(message)=>error.set(message),
                                            }
                                            busy.set(false);
                                        });
                                    }
                                },
                                "Build prerequisite plan"
                            }
                        }
                        if !transitions.is_empty(){
                            label{class:"form-row",
                                span{class:"label","Next learning phase"}
                                select{class:"field",disabled:busy(),
                                    value:"{next_phase()}",
                                    onchange:move |event|next_phase.set(event.value()),
                                    option{value:"","Select a phase"}
                                    for target in transitions {
                                        option{value:"{target}","{target}"}
                                    }
                                }
                            }
                            button{class:"btn btn-outline",r#type:"button",
                                disabled:busy()||!valid_identity(&session_id)
                                    ||!transitions.contains(&next_phase().as_str()),
                                onclick:{
                                    let id=session_id.clone();
                                    let allowed=transitions.to_vec();
                                    let on_saved=on_saved_advance.clone();
                                    move |_|{
                                        let target=next_phase();
                                        if busy()||!valid_identity(&id)
                                            ||!allowed.contains(&target.as_str()){
                                            return;
                                        }
                                        let args=json!({"session_id":id,
                                            "next_phase":target,
                                            "expected_revision":version});
                                        busy.set(true);error.set(String::new());
                                        spawn(async move{
                                            match mcp_app::call_learning_tool::<Value>(
                                                "advance_learning_session",args).await {
                                                Ok(_)=>{next_phase.set(String::new());on_saved.call(())},
                                                Err(message)=>error.set(message),
                                            }
                                            busy.set(false);
                                        });
                                    }
                                },
                                "Advance learning phase"
                            }
                        }
                    }
                    if session.get("steps").and_then(Value::as_array).is_some(){
                        p{class:"small muted","Reasoning steps: {list_records(&session).len()}"}
                        if let Some(steps)=session.get("steps").and_then(Value::as_array){
                            for step in steps{
                                p{class:"small muted",
                                    {field(step,&["title","concept_id","id"])}
                                    " — "
                                    {field(step,&["kind"])}
                                }
                            }
                        }
                        label{class:"form-row",
                            span{class:"label","Reasoning step ID"}
                            input{class:"field",maxlength:"128",value:"{step_id()}",
                                oninput:move |event|step_id.set(event.value())}
                        }
                        label{class:"form-row",
                            span{class:"label","Independent explanation"}
                            textarea{class:"field",maxlength:"8000",rows:"4",value:"{step_response()}",
                                oninput:move |event|step_response.set(event.value())}
                        }
                        label{class:"inline-row",
                            input{r#type:"checkbox",checked:consent(),onchange:move |event|consent.set(event.checked())}
                            span{"Permit this explanation to be disclosed to the configured model for provisional feedback."}
                        }
                        button{class:"btn btn-outline",r#type:"button",disabled:busy()||!consent(),
                            onclick:{
                                let session_id=session_id.clone();
                                move |_|{
                                    if busy()||!valid_identity(&session_id)||!valid_identity(&step_id())
                                        ||step_response().trim().is_empty(){return}
                                    let params=json!({"session_id":session_id,"step_id":step_id(),
                                        "learner_response":step_response(),"assisted":false,
                                        "allow_model_disclosure":true});
                                    busy.set(true);error.set(String::new());feedback.set(None);
                                    spawn(async move{
                                        match mcp_app::call_learning_tool::<Value>(
                                            "propose_step_model_assessment",params).await{
                                            Ok(value)=>feedback.set(Some(value)),
                                            Err(msg)=>error.set(msg),
                                        }
                                        busy.set(false);
                                    });
                                }
                            },
                            "Request provisional reasoning feedback"
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn EmbeddedSource(source_id:String)->Element{
    let mut extraction_consent=use_signal(||false);
    let mut extraction=use_signal(||None::<Value>);
    let mut preview=use_signal(||None::<Value>);
    let mut receipt=use_signal(||None::<Value>);
    let mut consent=use_signal(||false);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    rsx!{
        Card{
            title:"Audited source curriculum".to_owned(),
            description:"Import only reviewed claims and prerequisite links; no mastery is awarded.".to_owned(),
            div{class:"card-body stack",
                if !error().is_empty(){Notice{message:error()}}
                label{class:"inline-row",
                    input{r#type:"checkbox",checked:extraction_consent(),disabled:busy(),
                        onchange:move |event|extraction_consent.set(event.checked())}
                    span{"Disclose the next source passage to the configured model. Extracted claims will require independent audit."}
                }
                button{class:"btn btn-outline",r#type:"button",
                    disabled:busy()||!valid_identity(&source_id)||!extraction_consent(),
                    onclick:{
                        let source=source_id.clone();
                        move |_|{
                            if busy()||!extraction_consent()||!valid_identity(&source){return}
                            let args=json!({"source_id":source,
                                "allow_model_disclosure":true});
                            busy.set(true);error.set(String::new());extraction.set(None);
                            spawn(async move{
                                match mcp_app::call_learning_tool::<Value>(
                                    "stage_model_source_extraction",args).await{
                                    Ok(value)=>{
                                        extraction.set(Some(value));
                                        extraction_consent.set(false);
                                    }
                                    Err(msg)=>error.set(msg),
                                }
                                busy.set(false);
                            });
                        }
                    },
                    "Stage a model extraction for audit"
                }
                if let Some(value)=extraction(){
                    p{class:"small muted",
                        "The extraction is provisional; claims require a separate audit."
                    }
                    pre{class:"mono small",
                        "{serde_json::to_string_pretty(&value).unwrap_or_default()}"
                    }
                }
                button{class:"btn btn-outline",r#type:"button",
                    disabled:busy()||!valid_identity(&source_id),
                    onclick:{
                        let source=source_id.clone();
                        move |_|{
                            if busy()||!valid_identity(&source){return}
                            preview.set(None);receipt.set(None);consent.set(false);
                            busy.set(true);error.set(String::new());
                            let source=source.clone();
                            spawn(async move{
                                match mcp_app::call_learning_tool::<Value>(
                                    "preview_source_curriculum",json!({"source_id":source,"limit":80})).await{
                                    Ok(value)=>preview.set(Some(value)),
                                    Err(msg)=>error.set(msg),
                                }
                                busy.set(false);
                            });
                        }
                    },
                    "Preview source curriculum"
                }
                if let Some(data)=preview(){
                    pre{class:"mono small","{serde_json::to_string_pretty(&data).unwrap_or_default()}"}
                    if let Some(plan)=data.get("curriculum"){
                        if plan.get("ready").and_then(Value::as_bool)==Some(true){
                            if let Some(sha)=plan.get("sha256").and_then(Value::as_str){
                                if sha.len()==64&&sha.bytes().all(|b|
                                    b.is_ascii_digit()||(b'a'..=b'f').contains(&b)){
                                    label{class:"inline-row",
                                        input{r#type:"checkbox",checked:consent(),
                                            onchange:move |event|consent.set(event.checked())}
                                        span{"Import exactly these reviewed source concepts and prerequisites."}
                                    }
                                    button{class:"btn btn-primary",r#type:"button",
                                        disabled:busy()||!consent(),
                                        onclick:{
                                            let source=source_id.clone();
                                            let digest=sha.to_owned();
                                            move |_|{
                                                if busy()||!consent(){return}
                                                let params=json!({"source_id":source,
                                                    "limit":80,"expected_sha256":digest,"confirm":true});
                                                busy.set(true);error.set(String::new());
                                                spawn(async move{
                                                    match mcp_app::call_learning_tool::<Value>(
                                                        "import_source_curriculum",params).await{
                                                        Ok(value)=>{
                                                            receipt.set(Some(value));consent.set(false);
                                                        }
                                                        Err(msg)=>error.set(msg),
                                                    }
                                                    busy.set(false);
                                                });
                                            }
                                        },
                                        "Import confirmed curriculum"
                                    }
                                }
                            }
                        }
                    }
                }
                if let Some(data)=receipt(){
                    div{class:"alert",role:"status",
                        strong{"Source curriculum recorded"}
                        p{"Mastery remains unchanged."}
                        pre{class:"mono small","{serde_json::to_string_pretty(&data).unwrap_or_default()}"}
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
    fn learning_phase_controls_retain_legal_routes() {
        assert_eq!(permitted_session_phases("diagnose"),&["plan"]);
        assert_eq!(permitted_session_phases("plan"),&["diagnose","teach"]);
        assert_eq!(permitted_session_phases("practice"),&["teach","reassess"]);
        assert_eq!(permitted_session_phases("reassess"),&["diagnose","practice","complete"]);
        assert!(permitted_session_phases("complete").is_empty());
        assert!(permitted_session_phases("unrecognized").is_empty());
    }

    #[test]
    fn browser_scope_is_defined_by_fixed_learning_panels(){
        assert_eq!(Panel::Question.read(""),Err("Select a question identifier.".to_owned()));
        assert_eq!(Panel::Sessions.read("").unwrap().0,"list_learning_sessions");
        assert_eq!(Panel::from_view("learning_question"),Some(Panel::Question));
        assert_eq!(Panel::from_view("reasoning"),Some(Panel::Reasoning));
        assert_eq!(Panel::Sources.read("../oops").unwrap().0,"list_learning_sources");
        assert!(!valid_identity("../../etc/passwd"));
    }
}
