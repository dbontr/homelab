use dioxus::prelude::*;
use serde_json::Value;

use crate::api;
use crate::components::{Card, Empty, Heading, Loading, Notice};

#[component]
pub fn Learning() -> Element {
    let mut summary=use_resource(||async{api::get::<Value>("/v1/learning/summary").await});
    let mut sessions=use_resource(||async{api::get::<Value>("/v1/learning/sessions").await});
    let mut reviews=use_resource(||async{api::get::<Value>("/v1/learning/reviews").await});
    rsx!{
        Heading{
            title:"Learning".to_owned(),
            description:"Independent mastery, active reasoning sessions, and retention.".to_owned(),
            button{class:"btn btn-outline",onclick:move |_|{
                summary.restart();sessions.restart();reviews.restart();
            },"Refresh"}
        }
        Card{title:"Learning summary".to_owned(),
             description:"Mastery is determined by independently evaluated evidence.".to_owned(),
             match summary.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>rsx!{
                    div{class:"kv-list",
                        for (key,entry) in value.as_object().into_iter().flat_map(|data|data.iter()) {
                            div{class:"kv-entry",key:"{key}",
                                span{class:"small muted","{key.replace('_',\" \")}"},
                                strong{"{entry}"}
                            }
                        }
                    }
                }
             }
        }
        Card{title:"Active lessons".to_owned(),
             description:"Stored sessions preserve diagnostics and their reasoning routes.".to_owned(),
             match sessions.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let rows=api::list(&value,&["sessions","items"]);
                    rsx!{
                        if rows.is_empty(){Empty{title:"No sessions".to_owned(),
                            description:"Create a learning session through the Adaptive Teaching interface.".to_owned()}}
                        else{
                            div{class:"table-clip",
                                table{class:"table",
                                    thead{tr{th{"Goal"}th{"Phase"}th{"Mode"}th{"ID"}}}
                                    tbody{
                                        for record in rows{
                                            tr{
                                                td{ {api::property(&record,&["goal","title"])}}
                                                td{ {api::property(&record,&["phase"])}}
                                                td{ {api::property(&record,&["mode"])}}
                                                td{class:"mono muted",{api::property(&record,&["id"])}}
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
        Card{title:"Retention queue".to_owned(),
             description:"Due reviews are obligations, not evidence of completed learning.".to_owned(),
             match reviews.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(e))=>rsx!{Notice{message:e}},
                Some(Ok(value))=>{
                    let rows=api::list(&value,&["queue","reviews","items"]);
                    rsx!{
                        if rows.is_empty(){Empty{title:"No reviews currently due".to_owned(),
                            description:"Delayed reviews appear here when scheduled by Adaptive Teaching.".to_owned()}}
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
    }
}

#[component]
pub fn Health(csrf: String) -> Element{
    let mut proposals=use_resource(||async{api::get::<Value>("/v1/health/food-proposals").await});
    let mut health_busy=use_signal(||false);
    let mut health_error=use_signal(String::new);
    let mut plans=use_resource(||async{api::get::<Value>("/v1/health/plans").await});
    let mut records=use_resource(||async{api::get::<Value>("/v1/health/records").await});
    let mut goals=use_resource(||async{api::get::<Value>("/v1/health/goals").await});
    rsx!{
        Heading{
            title:"Health".to_owned(),
            description:"Training plans, recorded observations, and nutrient goals.".to_owned(),
            button{class:"btn btn-outline",onclick:move |_|{
                plans.restart();records.restart();goals.restart();proposals.restart();
            },"Refresh"}
        }
        if !health_error().is_empty(){Notice{message:health_error()}}
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
                                                "Nutrient amounts: {api::property(&item,&["nutrients"])}"
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
