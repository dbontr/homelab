//! Deterministic, keyboard-reachable rendering of a bounded Learning graph.
//! The graph is derived from read-only source data; selection changes only
//! the local focus identifier.
use dioxus::prelude::*;
use serde_json::Value;
use std::collections::HashMap;

const WIDTH:f64=880.0;
const HEIGHT:f64=520.0;

#[derive(Clone)]
struct GraphNode{
    id:String,
    title:String,
    x:f64,
    y:f64,
    level:u64,
}

fn layout(data:&Value)->(Vec<GraphNode>,Vec<(usize,usize,String)>) {
    let raw=data.get("nodes").and_then(Value::as_array)
        .cloned().unwrap_or_default();
    let count=raw.len().min(200);
    let mut nodes=Vec::with_capacity(count);
    let mut indexes=HashMap::new();
    for (index,item) in raw.into_iter().take(count).enumerate() {
        let Some(id)=item.get("id").and_then(Value::as_str) else{continue};
        if id.is_empty()||id.len()>128||indexes.contains_key(id){continue}
        let title=item.get("title").and_then(Value::as_str).unwrap_or(id)
            .chars().take(28).collect::<String>();
        // A bounded sunflower layout keeps all 200 nodes within the
        // viewport. Dense graphs use the accessible text controls for labels.
        let angle=index as f64*2.399_963_229_728_653;
        let radius=if count==1 {0.0} else {
            ((index as f64+0.5)/count as f64).sqrt()
        };
        let x=WIDTH/2.0+radius*(WIDTH/2.0-42.0)*angle.cos();
        let y=HEIGHT/2.0+radius*(HEIGHT/2.0-42.0)*angle.sin();
        let level=item.pointer("/mastery/level").and_then(Value::as_u64).unwrap_or(0);
        indexes.insert(id.to_owned(),nodes.len());
        nodes.push(GraphNode{id:id.to_owned(),title,x,y,level});
    }
    let mut edges=Vec::new();
    if let Some(raw_edges)=data.get("edges").and_then(Value::as_array){
        for item in raw_edges.iter().take(600) {
            let source=item.get("source").and_then(Value::as_str);
            let target=item.get("target").and_then(Value::as_str);
            if let (Some(a),Some(b))=(source.and_then(|s|indexes.get(s)),
                target.and_then(|s|indexes.get(s))) {
                if a!=b{
                    let kind=item.get("kind").and_then(Value::as_str)
                        .unwrap_or("related").to_owned();
                    edges.push((*a,*b,kind));
                }
            }
        }
    }
    (nodes,edges)
}

#[component]
pub fn LearningGraph(data:Value,on_select:EventHandler<String>)->Element{
    let mut zoom=use_signal(||1.0_f64);
    let mut pan_x=use_signal(||0.0_f64);
    let mut pan_y=use_signal(||0.0_f64);
    let mut search=use_signal(String::new);
    let (nodes,edges)=layout(&data);
    let scale=zoom().clamp(0.5,3.0);
    let width=WIDTH/scale;
    let height=HEIGHT/scale;
    let x=(WIDTH-width)/2.0+pan_x();
    let y=(HEIGHT-height)/2.0+pan_y();
    let bounds=format!("{x:.2} {y:.2} {width:.2} {height:.2}");
    let term=search().trim().to_lowercase();
    if nodes.is_empty(){return rsx!{p{class:"small muted","No graph nodes are available."}}}
    rsx! {
        div{class:"stack",
            p{class:"small muted",
                "Knowledge relationships. Use text controls to select a concept or navigate with keyboard focus."
            }
            label{class:"form-row",
                span{class:"label","Find a concept or proposition"}
                input{class:"field",maxlength:"128",value:"{search()}",
                    oninput:move |event|search.set(event.value())}
            }
            div{class:"inline-row",role:"group",aria_label:"Graph viewport",
                button{class:"btn btn-outline btn-sm",r#type:"button",
                    onclick:move |_|zoom.set((zoom()*1.25).min(3.0)),"Zoom in"}
                button{class:"btn btn-outline btn-sm",r#type:"button",
                    onclick:move |_|zoom.set((zoom()/1.25).max(0.5)),"Zoom out"}
                button{class:"btn btn-outline btn-sm",r#type:"button",
                    onclick:move |_|pan_x.set((pan_x()-60.0).max(-WIDTH/2.0)),"Pan left"}
                button{class:"btn btn-outline btn-sm",r#type:"button",
                    onclick:move |_|pan_x.set((pan_x()+60.0).min(WIDTH/2.0)),"Pan right"}
                button{class:"btn btn-outline btn-sm",r#type:"button",
                    onclick:move |_|pan_y.set((pan_y()-60.0).max(-HEIGHT/2.0)),"Pan up"}
                button{class:"btn btn-outline btn-sm",r#type:"button",
                    onclick:move |_|pan_y.set((pan_y()+60.0).min(HEIGHT/2.0)),"Pan down"}
                button{class:"btn btn-outline btn-sm",r#type:"button",
                    onclick:move |_|{
                        zoom.set(1.0);pan_x.set(0.0);pan_y.set(0.0);
                    },"Reset view"}
            }
            svg{
                role:"img",
                aria_label:"Concept relationship graph; all concepts are also available as text controls.",
                viewBox:"{bounds}",
                preserve_aspect_ratio:"xMidYMid meet",
                style:"display:block;width:100%;height:auto;max-height:500px;background:var(--surface);border:1px solid var(--border);border-radius:10px",
                for (index,(a,b,kind)) in edges.iter().enumerate(){
                    line{
                        key:"edge-{index}",
                        x1:"{nodes[*a].x}",y1:"{nodes[*a].y}",
                        x2:"{nodes[*b].x}",y2:"{nodes[*b].y}",
                        stroke:"currentColor",
                        stroke_opacity:if kind=="requires"{"0.55"}else{"0.2"},
                        stroke_dasharray:if kind=="requires"{"3 4"}else{"none"},
                    }
                }
                for node in &nodes{
                    g{key:"{node.id}",
                        circle{
                            cx:"{node.x}",cy:"{node.y}",r:"10",
                            fill:if node.level>=3{"#1e7d6b"}else{"#929eab"},
                        }
                        if nodes.len()<=20 {
                            text{
                                x:"{node.x+15.0}",y:"{node.y+3.0}",
                                fill:"currentColor",font_size:"11",
                                "{node.title}"
                            }
                        }
                    }
                }
            }
            div{class:"task-items",
                for node in nodes.into_iter().filter(|item|
                    term.is_empty()||item.id.to_lowercase().contains(&term)||
                    item.title.to_lowercase().contains(&term)){
                    button{
                        key:"{node.id}",
                        class:"btn btn-outline btn-sm",
                        r#type:"button",
                        onclick:{
                            let id=node.id.clone();
                            let on_select=on_select.clone();
                            move |_|on_select.call(id.clone())
                        },
                        "{node.title} · level {node.level}"
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
    fn largest_graph_fits_the_viewport() {
        let nodes=(0..250).map(|i|serde_json::json!({
            "id":format!("concept-{i}"),"title":format!("Concept {i}")
        })).collect::<Vec<_>>();
        let (layout_nodes,_)=layout(&serde_json::json!({"nodes":nodes}));
        assert_eq!(layout_nodes.len(),200);
        assert!(layout_nodes.iter().all(|n|
            n.x>=20.0&&n.x<=WIDTH-20.0&&
            n.y>=20.0&&n.y<=HEIGHT-20.0));
    }

    #[test]
    fn graph_links_are_restricted_to_known_nodes(){
        let input=serde_json::json!({
            "nodes":[{"id":"a","title":"A","mastery":{"level":3}},
                {"id":"b","title":"B"}],
            "edges":[{"source":"a","target":"b","kind":"requires"},
                {"source":"a","target":"outside","kind":"explains"}],
        });
        let (nodes,edges)=layout(&input);
        assert_eq!(nodes.len(),2);
        assert_eq!(edges.len(),1);
        assert_eq!(edges[0].2,"requires");
        assert_eq!(nodes[0].level,3);
    }
}
