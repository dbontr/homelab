package controller

import (
 "context"
 "errors"
 "sort"
)

type rankedPassage struct {
 Item map[string]any
 Score float64
 Sources []string
}
func fusePassages(lexical,semantic []map[string]any,limit int)[]map[string]any{
 combined:=map[string]*rankedPassage{}
 add:=func(rows []map[string]any,origin string){
  for i,passage:=range rows {
   id:=asString(passage["id"])
   citation:=asString(passage["citation_ref"])
   if id==""||citation==""{continue}
   record:=combined[id]
   if record==nil{
    copyOf:=make(map[string]any,len(passage)+3)
    for k,v:=range passage{copyOf[k]=v}
    record=&rankedPassage{Item:copyOf}
    combined[id]=record
   }
   record.Score+=1.0/float64(60+i+1)
   record.Sources=append(record.Sources,origin)
  }
 }
 add(lexical,"lexical")
 add(semantic,"semantic")
 ranked:=make([]*rankedPassage,0,len(combined))
 for _,r:=range combined{ranked=append(ranked,r)}
 sort.Slice(ranked,func(i,j int)bool{
  if ranked[i].Score!=ranked[j].Score{return ranked[i].Score>ranked[j].Score}
  return asString(ranked[i].Item["id"])<asString(ranked[j].Item["id"])
 })
 if len(ranked)>limit{ranked=ranked[:limit]}
 result:=make([]map[string]any,0,len(ranked))
 for _,item:=range ranked{
  item.Item["retrieval_sources"]=item.Sources
  item.Item["fusion_score"]=item.Score
  result=append(result,item.Item)
 }
 return result
}

// HybridResearch does not generate evidence or elevate embedding permissions.
// A degraded lexical-only result explicitly identifies absent semantic service.
func (s *Server) HybridResearch(ctx context.Context,project,query string,limit int)(map[string]any,error){
 if limit<1||limit>100||len(query)<2||len(query)>512{
  return nil,errors.New("invalid hybrid research scope")
 }
 lexical,err:=s.DB.SearchResearch(project,query,limit)
 if err!=nil{return nil,err}
 mode:="lexical_only"
 reason:="semantic provider or library consent unavailable"
 var semantic []map[string]any
 if s.Embedder!=nil{
  selected,err:=s.SemanticResearchQuery(ctx,project,query,limit)
  if err==nil{
   semantic=selected
   mode="hybrid"
   reason=""
  }
 }
 return map[string]any{
  "project_id":project,
  "mode":mode,
  "semantic_limitation":reason,
  "results":fusePassages(lexical,semantic,limit),
 },nil
}
