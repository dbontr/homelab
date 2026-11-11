use dioxus::prelude::*;
use serde_json::Value;

use crate::api;
use crate::components::{Card, Empty, Heading, Loading, Notice};

#[component]
pub fn Learning(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut summary=use_resource(||async{api::get::<Value>("/v1/learning/summary").await});
    let mut sessions=use_resource(||async{api::get::<Value>("/v1/learning/sessions").await});
    let mut reviews=use_resource(||async{api::get::<Value>("/v1/learning/reviews").await});
    let mut selected=use_signal(||None::<String>);
    let mut detail=use_resource(move ||{
        let id=selected();
        async move{
            match id{
                Some(session_id)=>api::get::<Value>(&format!("/v1/learning/sessions/{session_id}")).await,
                None=>Ok(Value::Null)
            }
        }
    });
    let mut pending=use_resource(move ||{
        let id=selected();
        async move{
            match id{
                Some(session_id)=>api::get::<Value>(&format!("/v1/learning/sessions/{session_id}/pending")).await,
                None=>Ok(Value::Null)
            }
        }
    });
    let mut goal=use_signal(String::new);
    let mut target=use_signal(String::new);
    let mut mode=use_signal(||"learn".to_owned());
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    let mut notice=use_signal(String::new);
    let current_question=pending.cloned().and_then(Result::ok).and_then(|value|
        if value.get("pending").and_then(Value::as_bool)==Some(true) {
            value.get("question").cloned()
        } else {None}
    );
    let current_question_id = current_question.as_ref()
        .and_then(|question| question.get("id").and_then(Value::as_str))
        .unwrap_or("").to_owned();
    rsx!{
        Heading{
            title:"Learning".to_owned(),
            description:"Build durable understanding through independent reasoning and delayed reassessment.".to_owned(),
            button{class:"btn btn-outline",onclick:move |_|{
                summary.restart();sessions.restart();reviews.restart();detail.restart();pending.restart();
            },"Refresh"}
        }
        if !error().is_empty(){Notice{message:error()}}
        if !notice().is_empty(){div{class:"alert",role:"status","{notice()}"}}
        section{class:"card",
            div{class:"card-header",
                h2{class:"card-title","Start a learning session"}
                p{class:"card-description","Select an existing concept ID from Adaptive Teaching; progress is earned by your work, not by creating a session."}
            }
            form{class:"card-body task-editor-fields",
                onsubmit:move |event|{
                    event.prevent_default();
                    if busy(){return}
                    let payload=serde_json::json!({
                        "goal":goal(),"target_concept_id":target(),"mode":mode()
                    });
                    let token=csrf();
                    busy.set(true);error.set(String::new());
                    spawn(async move{
                        match api::write::<Value,_>(reqwest::Method::POST,"/v1/learning/sessions",&token,&payload).await{
                            Ok(record)=>{
                                if let Some(id)=record.get("id").and_then(Value::as_str){
                                    selected.set(Some(id.to_owned()));
                                }
                                sessions.restart();detail.restart();pending.restart();
                                notice.set("Learning session created".to_owned());
                            },
                            Err(e)=>error.set(e),
                        }
                        busy.set(false);
                    });
                },
                label{class:"form-row",
                    span{class:"label","Learning goal"}
                    input{class:"field",required:true,maxlength:"500",value:"{goal()}",
                        placeholder:"Derive Maxwell's equations from the underlying laws",
                        oninput:move |e|goal.set(e.value())}
                }
                div{class:"task-field-grid",
                    label{class:"form-row",
                        span{class:"label","Existing concept ID"}
                        input{class:"field",required:true,value:"{target()}",
                            placeholder:"maxwells-equations",
                            oninput:move |e|target.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Mode"}
                        select{class:"field",value:"{mode()}",
                            onchange:move |e|mode.set(e.value()),
                            option{value:"learn","Learn"}
                            option{value:"practise","Practice"}
                            option{value:"solve","Solve"}
                            option{value:"assess","Assess"}
                        }
                    }
                }
                button{class:"btn btn-primary",disabled:busy(),"Start session"}
            }
        }
        Card{title:"Learning sessions".to_owned(),
             description:"Select a session to inspect its phase and pending question.".to_owned(),
             match sessions.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let rows=api::list(&value,&["sessions","items"]);
                    rsx!{
                        if rows.is_empty(){Empty{title:"No sessions".to_owned(),
                            description:"Create a learning session after defining a concept.".to_owned()}}
                        else{
                            div{class:"task-items",
                                for record in rows{
                                    div{class:"task-item",
                                        div{class:"task-information",
                                            span{class:"task-title",{api::property(&record,&["goal","title"])}}
                                            span{class:"small muted",{api::property(&record,&["phase","mode"])}}
                                        }
                                        button{class:"btn btn-outline btn-sm",
                                            onclick:{
                                                let id=api::property(&record,&["id"]);
                                                move |_|selected.set(Some(id.clone()))
                                            },
                                            "Open"
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
             }
        }
        if let Some(session_id)=selected(){
            Card{title:"Selected session".to_owned(),
                 description:format!("Session {session_id}"),
                 match detail.cloned(){
                    None=>rsx!{Loading{}},
                    Some(Err(e))=>rsx!{Notice{message:e}},
                    Some(Ok(value))=>rsx!{
                        div{class:"kv-list",
                            div{class:"kv-entry",span{class:"small muted","Goal"},
                                strong{ {api::property(&value,&["goal"])} }}
                            div{class:"kv-entry",span{class:"small muted","Phase"},
                                strong{ {api::property(&value,&["phase"])} }}
                            div{class:"kv-entry",span{class:"small muted","Revision"},
                                strong{ {api::property(&value,&["revision"])} }}
                        }
                    }
                 }
            }
            if let Some(Ok(value)) = detail.cloned() {
                crate::learning_route::LearningRoute {
                    key: "{session_id}",
                    csrf: csrf(),
                    session_id: session_id.clone(),
                    detail: value,
                    on_saved: move |_| {
                        detail.restart();
                        pending.restart();
                        sessions.restart();
                        reviews.restart();
                    }
                }
            }
            crate::learning_attachments::LearningAttachments {
                key: "{session_id}",
                csrf: csrf(),
                owner_kind: "session".to_owned(),
                owner_id: session_id.clone(),
            }
            if let Some(question)=current_question{
                LearningQuestion {
                    key: "{current_question_id}",
                    csrf: csrf(),
                    question,
                    on_saved: move |_| {
                        pending.restart();
                        detail.restart();
                        sessions.restart();
                        reviews.restart();
                        notice.set("Your response is saved. Independent assessment is still required.".to_owned());
                    }
                }
            } else {
                Card{title:"Pending work".to_owned(),
                    description:"No ungraded learning question is available.".to_owned(),
                    div{class:"card-body",
                        p{class:"small muted","Continue planning and asking questions through Adaptive Teaching."}
                    }
                }
            }
        }
        Card{title:"Retention queue".to_owned(),
             description:"Due reviews do not establish mastery until independently assessed.".to_owned(),
             match reviews.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let rows=api::list(&value,&["queue","reviews","items"]);
                    rsx!{
                        if rows.is_empty(){Empty{title:"No reviews currently due".to_owned(),
                            description:"Delayed reviews appear when scheduled by Adaptive Teaching.".to_owned()}}
                        else{
                            div{class:"table-clip",
                                table{class:"table",
                                    thead{tr{th{"Concept"}th{"Due"}th{"Reason"}}}
                                    tbody{
                                        for record in rows{
                                            tr{
                                                td{ {api::property(&record,&["concept_title","concept_id","title"])}}
                                                td{class:"muted",{api::property(&record,&["due_at","due","due_date"])}}
                                                td{class:"muted",{api::property(&record,&["reason","miss_class"])}}
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
        Card{title:"Mastery evidence".to_owned(),
             description:"A summary of independently demonstrated knowledge.".to_owned(),
             match summary.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>rsx!{
                    div{class:"kv-list",
                        for (key,entry) in value.as_object().into_iter().flat_map(|data|data.iter()) {
                            div{class:"kv-entry",key:"{key}",
                                span{class:"small muted",{key.replace('_'," ")}},
                                strong{"{entry}"}
                            }
                        }
                    }
                }
             }
        }
    }
}

#[component]
pub fn Health(csrf: String) -> Element{
    let csrf = use_signal(|| csrf);
    let mut proposals=use_resource(||async{api::get::<Value>("/v1/health/food-proposals").await});
    let mut health_busy=use_signal(||false);
    let mut health_error=use_signal(String::new);
    let mut plans=use_resource(||async{api::get::<Value>("/v1/health/plans").await});
    let mut records=use_resource(||async{api::get::<Value>("/v1/health/records").await});
    let mut goals=use_resource(||async{api::get::<Value>("/v1/health/goals").await});
    let mut sets=use_resource(||async{api::get::<Value>("/v1/health/training-sets").await});
    let mut exercise=use_signal(String::new);
    let mut activity=use_signal(||"strength".to_owned());
    let mut training_plan=use_signal(String::new);
    let mut training_session=use_signal(String::new);
    let mut set_number=use_signal(||"1".to_owned());
    let mut repetitions=use_signal(||"8".to_owned());
    let mut load=use_signal(String::new);
    let mut duration=use_signal(String::new);
    let mut distance=use_signal(String::new);
    let mut rpe=use_signal(String::new);
    let mut progression=use_signal(||None::<Value>);
    let mut obs_domain=use_signal(||"biometric".to_owned());
    let mut obs_metric=use_signal(||"weight".to_owned());
    let mut obs_value=use_signal(String::new);
    let mut obs_unit=use_signal(||"kg".to_owned());
    let mut obs_method=use_signal(||"measured".to_owned());
    rsx!{
        Heading{
            title:"Health".to_owned(),
            description:"Training plans, recorded observations, and nutrient goals.".to_owned(),
            button{class:"btn btn-outline",onclick:move |_|{
                plans.restart();records.restart();goals.restart();proposals.restart();sets.restart();
            },"Refresh"}
        }
        if !health_error().is_empty(){Notice{message:health_error()}}
        section{class:"card",
            div{class:"card-header",
                h2{class:"card-title","Log training"}
                p{class:"card-description","Record performed work; missing logs are not interpreted as missed workouts."}
            }
            form{class:"card-body task-editor-fields",
                onsubmit:move |event|{
                    event.prevent_default();
                    if health_busy(){return}
                    let Some(index)=set_number().parse::<i64>().ok() else {
                        health_error.set("Enter a valid set number".to_owned());return
                    };
                    let Some(reps)=repetitions().parse::<i64>().ok() else {
                        health_error.set("Enter valid repetitions".to_owned());return
                    };
                    let parse_optional=|value:String| -> Result<Option<f64>,String>{
                        if value.trim().is_empty(){return Ok(None)}
                        match value.parse::<f64>(){
                            Ok(n) if n.is_finite()&&n>=0.0 => Ok(Some(n)),
                            _=>Err("Training measurements must be finite and nonnegative".to_owned()),
                        }
                    };
                    let Ok(load_kg)=parse_optional(load()) else {
                        health_error.set("Invalid load".to_owned());return
                    };
                    let Ok(seconds)=parse_optional(duration()) else {
                        health_error.set("Invalid duration".to_owned());return
                    };
                    let Ok(meters)=parse_optional(distance()) else {
                        health_error.set("Invalid distance".to_owned());return
                    };
                    let Ok(exertion)=parse_optional(rpe()) else {
                        health_error.set("Invalid RPE".to_owned());return
                    };
                    let payload=serde_json::json!({
                        "exercise":exercise(),"activity":activity(),"plan_id":training_plan(),
                        "session_id":training_session(),"set_number":index,
                        "repetitions":reps,"load_kg":load_kg,"duration_seconds":seconds,
                        "distance_meters":meters,"rpe":exertion
                    });
                    let token=csrf();
                    health_error.set(String::new());health_busy.set(true);
                    spawn(async move{
                        match api::write::<Value,_>(reqwest::Method::POST,"/v1/health/training-sets",&token,&payload).await{
                            Ok(_)=>{sets.restart();},
                            Err(e)=>health_error.set(e),
                        }
                        health_busy.set(false);
                    });
                },
                div{class:"task-field-grid",
                    label{class:"form-row",
                        span{class:"label","Plan ID (optional)"}
                        input{class:"field",value:"{training_plan()}",oninput:move |e|training_plan.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Session ID (optional)"}
                        input{class:"field",value:"{training_session()}",oninput:move |e|training_session.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Exercise"}
                        input{class:"field",required:true,maxlength:"200",
                            placeholder:"Squat, rowing, mobility...",
                            value:"{exercise()}",oninput:move |e|exercise.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Activity"}
                        select{class:"field",value:"{activity()}",
                            onchange:move |e|activity.set(e.value()),
                            option{value:"strength","Strength"}
                            option{value:"cardio","Cardio"}
                            option{value:"mobility","Mobility"}
                        }
                    }
                    label{class:"form-row",
                        span{class:"label","Set number"}
                        input{class:"field",r#type:"number",min:"0",max:"1000",value:"{set_number()}",
                            oninput:move |e|set_number.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Repetitions"}
                        input{class:"field",r#type:"number",min:"0",value:"{repetitions()}",
                            oninput:move |e|repetitions.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Load (kg)"}
                        input{class:"field",r#type:"number",min:"0",step:"any",
                            value:"{load()}",oninput:move |e|load.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Duration (seconds)"}
                        input{class:"field",r#type:"number",min:"0",step:"any",
                            value:"{duration()}",oninput:move |e|duration.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Distance (meters)"}
                        input{class:"field",r#type:"number",min:"0",step:"any",
                            value:"{distance()}",oninput:move |e|distance.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","RPE (0–10)"}
                        input{class:"field",r#type:"number",min:"0",max:"10",step:"any",
                            value:"{rpe()}",oninput:move |e|rpe.set(e.value())}
                    }
                }
                div{class:"task-actions",
                    button{class:"btn btn-primary",disabled:health_busy(),"Save set or interval"}
                    button{class:"btn btn-outline",r#type:"button",disabled:health_busy() || exercise().trim().is_empty(),
                        onclick:move |_|{
                            let Some(reps)=repetitions().parse::<i64>().ok().filter(|n|*n>=1&&*n<=30)
                            else{health_error.set("Progression review requires a repetition target from 1 to 30".to_owned());return};
                            let encoded=exercise().bytes().map(|b|{
                                if b.is_ascii_alphanumeric() || b"-_.~".contains(&b){
                                    (b as char).to_string()
                                }else{format!("%{b:02X}")}
                            }).collect::<String>();
                            let path=format!("/v1/health/training-progress?exercise={encoded}&repetitions={reps}");
                            health_busy.set(true);health_error.set(String::new());
                            spawn(async move{
                                match api::get::<Value>(&path).await{
                                    Ok(value)=>progression.set(Some(value)),
                                    Err(e)=>health_error.set(e),
                                }
                                health_busy.set(false);
                            });
                        },
                        "Review progression"
                    }
                }
                if let Some(preview)=progression(){
                    div{class:"alert",role:"status",
                        p{strong{ {api::property(&preview,&["status"])} }}
                        p{class:"small muted",{api::property(&preview,&["rationale"])}}
                        p{"Candidate load (kg): ", {api::property(&preview,&["candidate_load_kg"])}}
                        p{class:"small muted",{api::property(&preview,&["warning"])}}
                        p{class:"small muted","No training plan has been changed."}
                    }
                }
            }
        }
        Card{title:"Recent training".to_owned(),
            description:"Logged training entries with their original source and intensity.".to_owned(),
            match sets.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let items=api::list(&value,&["sets","items"]);
                    rsx!{
                        if items.is_empty(){Empty{
                            title:"No recorded training".to_owned(),
                            description:"Log a performed set or interval above.".to_owned()
                        }}
                        else{
                            div{class:"table-clip",
                                table{class:"table",
                                    thead{tr{th{"Exercise"}th{"Reps"}th{"Load"}th{"RPE"}th{"Source"}}}
                                    tbody{
                                        for item in items.into_iter().take(50){
                                            tr{
                                                td{ {api::property(&item,&["exercise"])} }
                                                td{ {api::property(&item,&["repetitions"])} }
                                                td{ {api::property(&item,&["load_kg"])} }
                                                td{ {api::property(&item,&["rpe"])} }
                                                td{class:"muted",{api::property(&item,&["source"])} }
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
        section{class:"card",
            div{class:"card-header",
                h2{class:"card-title","Record an observation"}
                p{class:"card-description","Save only values actually observed or explicitly estimated."}
            }
            form{class:"card-body task-editor-fields",
                onsubmit:move |event|{
                    event.prevent_default();
                    if health_busy(){return}
                    let Ok(value)=obs_value().parse::<f64>() else {
                        health_error.set("Enter a numeric observation".to_owned());return
                    };
                    if !value.is_finite(){health_error.set("The observation must be finite".to_owned());return}
                    let request=serde_json::json!({
                        "domain":obs_domain(),"metric":obs_metric(),"value":value,
                        "unit":obs_unit(),"method":obs_method()
                    });
                    let token=csrf();
                    health_busy.set(true);health_error.set(String::new());
                    spawn(async move{
                        match api::write::<Value,_>(reqwest::Method::POST,"/v1/health/records",&token,&request).await{
                            Ok(_)=>{obs_value.set(String::new());records.restart();},
                            Err(error)=>health_error.set(error),
                        }
                        health_busy.set(false);
                    });
                },
                div{class:"task-field-grid",
                    label{class:"form-row",
                        span{class:"label","Domain"}
                        select{class:"field",value:"{obs_domain()}",
                            onchange:move |e|obs_domain.set(e.value()),
                            option{value:"biometric","Biometric"}
                            option{value:"nutrition","Nutrition"}
                            option{value:"sleep","Sleep"}
                            option{value:"workout","Workout"}
                            option{value:"symptom","Symptom"}
                        }
                    }
                    label{class:"form-row",
                        span{class:"label","Metric"}
                        input{class:"field",required:true,value:"{obs_metric()}",
                            oninput:move |e|obs_metric.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Value"}
                        input{class:"field",r#type:"number",step:"any",required:true,value:"{obs_value()}",
                            oninput:move |e|obs_value.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Unit"}
                        input{class:"field",required:true,value:"{obs_unit()}",
                            oninput:move |e|obs_unit.set(e.value())}
                    }
                    label{class:"form-row",
                        span{class:"label","Method"}
                        select{class:"field",value:"{obs_method()}",
                            onchange:move |e|obs_method.set(e.value()),
                            option{value:"measured","Measured"}
                            option{value:"reported","Reported"}
                            option{value:"estimated","Estimated"}
                        }
                    }
                }
                button{class:"btn btn-primary",disabled:health_busy(),"Record"}
            }
        }
        Card{
            title:"Food proposals".to_owned(),
            description:"Confirm identification, portions, and nutrient evidence before adding the meal to Health.".to_owned(),
            match proposals.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let items=api::list(&value,&["proposals","items"]);
                    rsx!{
                        if items.is_empty(){Empty{
                            title:"No food proposals".to_owned(),
                            description:"A recipe proposal or other unconfirmed food will appear here.".to_owned()
                        }}
                        else{
                            div{class:"task-items",
                                for item in items{
                                    div{class:"task-item",
                                        div{class:"task-information",
                                            span{class:"task-title",{api::property(&item,&["food"])}}
                                            span{class:"small muted",{api::property(&item,&["portion"])}}
                                            span{class:"small muted",{api::property(&item,&["notes"])}}
                                            span{class:"small muted",
                                                "Nutrients: ", {item.get("nutrients").map(|n|n.to_string()).unwrap_or_else(||"Unavailable".to_owned())}
                                            }
                                        }
                                        if api::property(&item,&["status"])=="pending"{
                                            button{
                                                class:"btn btn-outline btn-sm",disabled:health_busy(),
                                                onclick:{
                                                    let id=api::property(&item,&["id"]);
                                                    let token=csrf();
                                                    move |_|{
                                                        let id=id.clone();
                                                        let token=token.clone();
                                                        health_busy.set(true);
                                                        health_error.set(String::new());
                                                        spawn(async move{
                                                            match api::write::<Value,_>(
                                                                reqwest::Method::POST,
                                                                &format!("/v1/health/food-proposals/{id}/confirm"),
                                                                &token,&serde_json::json!({"confirm":true})
                                                            ).await{
                                                                Ok(_) => {proposals.restart();records.restart();},
                                                                Err(e) => health_error.set(e),
                                                            }
                                                            health_busy.set(false);
                                                        });
                                                    }
                                                },
                                                "Confirm meal"
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
        Card{
            title:"Health goals".to_owned(),
            description:"Recorded targets; unavailable nutrients are not interpreted as zero.".to_owned(),
            match goals.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>rsx!{
                    div{class:"kv-list",
                        for (name,entry) in value.as_object().into_iter().flat_map(|data|data.iter()){
                            div{class:"kv-entry",key:"{name}",
                                span{class:"small muted","{name.replace('_',\" \")}"},
                                strong{ "{entry}" }
                            }
                        }
                    }
                }
            }
        }
        Card{
            title:"Training and meal plans".to_owned(),
            description:"Workout completion remains unconfirmed without a log or explicit decision.".to_owned(),
            match plans.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let items=api::list(&value,&["plans","items"]);
                    rsx!{
                        if items.is_empty(){Empty{
                            title:"No plans recorded".to_owned(),
                            description:"Create a plan through the Health service.".to_owned()
                        }}
                        else{
                            div{class:"table-clip",
                                table{class:"table",
                                    thead{tr{th{"Plan"}th{"Kind"}th{"Revision"}}}
                                    tbody{
                                        for item in items{
                                            tr{
                                                td{ {api::property(&item,&["title","name"])} }
                                                td{ {api::property(&item,&["kind"])} }
                                                td{class:"muted",{api::property(&item,&["revision"])} }
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
        Card{
            title:"Recent observations".to_owned(),
            description:"These records retain the source and measurement method where supplied.".to_owned(),
            match records.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let items=api::list(&value,&["records","observations","items"]);
                    rsx!{
                        if items.is_empty(){Empty{
                            title:"No observations available".to_owned(),
                            description:"Add measurements using the Health interface.".to_owned()
                        }}
                        else{
                            div{class:"table-clip",
                                table{class:"table",
                                    thead{tr{th{"Metric"}th{"Value"}th{"Unit"}th{"Source"}th{"Method"}}}
                                    tbody{
                                        for item in items.into_iter().take(50){
                                            tr{
                                                td{ {api::property(&item,&["metric"])} }
                                                td{ {api::property(&item,&["value"])} }
                                                td{ {api::property(&item,&["unit"])} }
                                                td{class:"muted",{api::property(&item,&["source"])} }
                                                td{class:"muted",{api::property(&item,&["method"])} }
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

fn valid_learning_answer(question: &Value, response: &str) -> bool {
    let Some(id) = question.get("id").and_then(Value::as_str) else {
        return false;
    };
    if id.is_empty() || id.len() > 128
        || !id.bytes().all(|byte| byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'_')
        || response.trim().is_empty() || response.len() > 12000
        || question.get("status").and_then(Value::as_str) != Some("pending")
    {
        return false;
    }
    match question.get("response_kind").and_then(Value::as_str) {
        Some("multiple_choice") => question.get("options").and_then(Value::as_array)
            .is_some_and(|options| options.iter().any(|option|
                option.get("id").and_then(Value::as_str) == Some(response))),
        Some("free_response") => true,
        _ => false,
    }
}

#[component]
fn LearningQuestion(question: Value, csrf: String, on_saved: EventHandler<()>) -> Element {
    let mut answer = use_signal(String::new);
    let mut reasoning = use_signal(String::new);
    let mut confidence = use_signal(String::new);
    let mut assisted = use_signal(|| false);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);

    let id = question.get("id").and_then(Value::as_str).unwrap_or("").to_owned();
    let status = question.get("status").and_then(Value::as_str).unwrap_or("");
    let multiple_choice = question.get("response_kind").and_then(Value::as_str)
        == Some("multiple_choice");
    let options = question.get("options").and_then(Value::as_array).cloned()
        .unwrap_or_default();
    let saved_response = question.get("learner_response").and_then(Value::as_str)
        .unwrap_or("");
    let saved_option = options.iter()
        .find(|option| option.get("id").and_then(Value::as_str) == Some(saved_response))
        .and_then(|option| option.get("label").and_then(Value::as_str));
    let saved_confidence = question.get("self_confidence").and_then(Value::as_f64)
        .map(|value| format!("{:.0}%", value * 100.0));

    rsx! {
        Card {
            title: "Learning question".to_owned(),
            description: "An answer is saved before a separate evaluator assesses correctness and reasoning.".to_owned(),
            div { class: "card-body stack",
                p { {api::property(&question, &["prompt"])} }
                if !error().is_empty() { Notice { message: error() } }
                if status == "answered" {
                    div { role: "status", class: "alert",
                        "Response recorded. Independent assessment is pending."
                    }
                    div { class: "kv-list",
                        div { class: "kv-entry",
                            span { class: "small muted", "Your answer" }
                            span { "{saved_option.unwrap_or(saved_response)}" }
                        }
                        if let Some(explanation) = question.get("learner_reasoning")
                            .and_then(Value::as_str).filter(|value| !value.is_empty()) {
                            div { class: "kv-entry",
                                span { class: "small muted", "Your reasoning" }
                                span { "{explanation}" }
                            }
                        }
                        if let Some(value) = saved_confidence {
                            div { class: "kv-entry",
                                span { class: "small muted", "Your confidence" }
                                span { "{value}" }
                            }
                        }
                        div { class: "kv-entry",
                            span { class: "small muted", "Assistance used" }
                            span { if question.get("assisted").and_then(Value::as_bool) == Some(true) {
                                "Yes"
                            } else {
                                "No"
                            } }
                        }
                    }
                    p { class: "small muted",
                        "The committed answer key remains sealed. This record does not establish mastery."
                    }
                } else if status == "pending" {
                    form {
                        class: "task-editor-fields",
                        onsubmit: {
                            let id = id.clone();
                            let token = csrf.clone();
                            let question = question.clone();
                            move |event| {
                                event.prevent_default();
                                if busy() { return; }
                                let response = answer();
                                if !valid_learning_answer(&question, &response) {
                                    error.set("Select a valid answer before submitting.".to_owned());
                                    return;
                                }
                                let explanation = reasoning();
                                if response.len() > 12000 || explanation.len() > 16000 {
                                    error.set("The response exceeds the accepted length.".to_owned());
                                    return;
                                }
                                let belief = confidence().parse::<f64>().ok()
                                    .filter(|value| value.is_finite() && (0.0..=1.0).contains(value));
                                let payload = serde_json::json!({
                                    "learner_response": response,
                                    "learner_reasoning": explanation,
                                    "assisted": assisted(),
                                    "self_confidence": belief,
                                });
                                let url = format!("/v1/learning/questions/{id}/answer");
                                let token = token.clone();
                                error.set(String::new());
                                busy.set(true);
                                spawn(async move {
                                    match api::write::<Value, _>(
                                        reqwest::Method::POST, &url, &token, &payload
                                    ).await {
                                        Ok(_) => on_saved.call(()),
                                        Err(message) => error.set(format!(
                                            "The response was not saved: {message}. Refresh before retrying."
                                        )),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        if multiple_choice {
                            label { class: "form-row",
                                span { class: "label", "Your answer" }
                                select {
                                    class: "field",
                                    required: true,
                                    disabled: busy(),
                                    value: "{answer()}",
                                    onchange: move |event| answer.set(event.value()),
                                    option { value: "", "Choose an answer" }
                                    for option in &options {
                                        if let (Some(value), Some(title)) = (
                                            option.get("id").and_then(Value::as_str),
                                            option.get("label").and_then(Value::as_str)
                                        ) {
                                            option { value: "{value}", "{title}" }
                                        }
                                    }
                                }
                            }
                        } else {
                            label { class: "form-row",
                                span { class: "label", "Your answer" }
                                textarea {
                                    class: "field", required: true, rows: "4",
                                    maxlength: "12000", disabled: busy(),
                                    value: "{answer()}",
                                    oninput: move |event| answer.set(event.value())
                                }
                            }
                        }
                        label { class: "form-row",
                            span { class: "label", "Your reasoning" }
                            textarea {
                                class: "field", rows: "4", maxlength: "16000", disabled: busy(),
                                value: "{reasoning()}",
                                oninput: move |event| reasoning.set(event.value())
                            }
                        }
                        label { class: "form-row",
                            span { class: "label", "Confidence (optional)" }
                            select {
                                class: "field", disabled: busy(),
                                value: "{confidence()}",
                                onchange: move |event| confidence.set(event.value()),
                                option { value: "", "Not specified" }
                                option { value: "0.0", "0% — certain it is incorrect" }
                                option { value: "0.25", "25%" }
                                option { value: "0.5", "50%" }
                                option { value: "0.75", "75%" }
                                option { value: "1.0", "100% — certain it is correct" }
                            }
                        }
                        label { class: "inline-row",
                            input {
                                r#type: "checkbox", checked: assisted(), disabled: busy(),
                                onchange: move |event| assisted.set(event.checked())
                            }
                            span { "I used assistance" }
                        }
                        button {
                            class: "btn btn-primary",
                            disabled: busy() || answer().trim().is_empty(),
                            "Save answer"
                        }
                        p { class: "small muted",
                            "Your answer, reasoning, confidence and assistance report are preserved together. Submission does not grade the response."
                        }
                    }
                } else {
                    p { class: "small muted",
                        "This question is not awaiting an answer. Refresh its Learning session to inspect the latest state."
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod learning_question_tests {
    use super::*;

    #[test]
    fn accepts_only_a_pending_question_and_a_committed_choice() {
        let mut question = serde_json::json!({
            "id": "question-1",
            "status": "pending",
            "response_kind": "multiple_choice",
            "options": [{"id": "a", "label": "First"}, {"id": "b", "label": "Second"}],
        });
        assert!(valid_learning_answer(&question, "b"));
        assert!(!valid_learning_answer(&question, "uncommitted"));
        assert!(!valid_learning_answer(&question, ""));
        question["status"] = serde_json::json!("answered");
        assert!(!valid_learning_answer(&question, "a"));
        question["status"] = serde_json::json!("pending");
        question["id"] = serde_json::json!("../another-session");
        assert!(!valid_learning_answer(&question, "b"));
    }

    #[test]
    fn free_response_has_separate_bounds_and_cannot_use_unknown_question_types() {
        let mut question = serde_json::json!({
            "id": "question_2", "status": "pending", "response_kind": "free_response"
        });
        assert!(valid_learning_answer(&question, "A derivation with explicit assumptions"));
        assert!(!valid_learning_answer(&question, "  "));
        assert!(!valid_learning_answer(&question, &"a".repeat(12001)));
        question["response_kind"] = serde_json::json!("unrecognized");
        assert!(!valid_learning_answer(&question, "answer"));
    }
}
