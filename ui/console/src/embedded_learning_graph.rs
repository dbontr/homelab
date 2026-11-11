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
        let ring=index/24;
        let within=index%24;
        let ring_start=ring*24;
        let slots=(count-ring_start).min(24).max(1);
        let angle=std::f64::consts::TAU*(within as f64/slots as f64)
            -std::f64::consts::FRAC_PI_2;
        let radius=70.0+70.0*ring as f64;
        let x=WIDTH/2.0+radius*angle.cos();
        let y=HEIGHT/2.0+radius*angle.sin();
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
    let (nodes,edges)=layout(&data);
    if nodes.is_empty(){return rsx!{p{class:"small muted","No graph nodes are available."}}}
    rsx! {
        div{class:"stack",
            p{class:"small muted",
                "Knowledge relationships. Select any concept below to focus on it."
            }
            svg{
                role:"img",
                aria_label:"Concept relationship graph; all concepts are also available as text controls.",
                viewBox:"0 0 880 520",
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
                        text{
                            x:"{node.x+15.0}",y:"{node.y+3.0}",
                            fill:"currentColor",font_size:"11",
                            "{node.title}"
                        }
                    }
                }
            }
            div{class:"task-items",
                for node in nodes{
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
