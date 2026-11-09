package store

import (
 "encoding/json"
 "errors"
 "sort"
 "strings"
)

// SourceCurriculum describes a reviewable route from source-grounded claims to
// assessable concepts. It never represents claimed knowledge as learner mastery.
type SourceCurriculum struct {
 SourceID string `json:"source_id"`
 SourceLabel string `json:"source_label"`
 SourceSHA256 string `json:"source_sha256"`
 Steps []SourceCurriculumStep `json:"steps"`
 RemainingSpans int `json:"remaining_spans"`
 UnreviewedClaims int `json:"unreviewed_claims"`
 Blockers []string `json:"blockers"`
 Ready bool `json:"ready"`
 SHA256 string `json:"sha256"`
}
type SourceCurriculumStep struct {
 PropositionID string `json:"proposition_id"`
 ConceptID string `json:"concept_id"`
 Title string `json:"title"`
 Statement string `json:"statement"`
 Context string `json:"context"`
 Domain string `json:"domain"`
 SourceSpanID string `json:"source_span_id"`
 Quote string `json:"quote"`
 Dependencies []SourceCurriculumDependency `json:"dependencies"`
}
type SourceCurriculumDependency struct {
 ConceptID string `json:"concept_id"`
 ChainID string `json:"chain_id"`
 Rationale string `json:"rationale"`
}

func sourceConceptID(propositionID string)string{
 return "source-"+fingerprint("concept",propositionID)[:24]
}
func curriculumTitle(statement string)string{
 runes:=[]rune(strings.TrimSpace(statement))
 if len(runes)>120 {runes=runes[:120]}
 return string(runes)
}
func curriculumSnapshot(db *database,sourceID string,limit int)(SourceCurriculum,error){
 var out SourceCurriculum
 if limit<1||limit>200{return out,errors.New("source curriculum limit must be from 1 to 200")}
 source:=getRow(db,"SELECT * FROM knowledge_sources WHERE id=?",sourceID)
 if source==nil{return out,errors.New("learning source is not available")}
 if strVal(source["kind"])!="conversation" && fingerprint(strVal(source["kind"]),strVal(source["content"]))!=strVal(source["sha256"]){
  return out,errors.New("source content does not match its original digest")
 }
 out=SourceCurriculum{
  SourceID:sourceID,SourceLabel:strVal(source["label"]),
  SourceSHA256:strVal(source["sha256"]),
  Steps:[]SourceCurriculumStep{},Blockers:[]string{},
 }
 missing:=getRow(db,"SELECT COUNT(*) AS total FROM source_spans WHERE source_id=? AND processed_at IS NULL",sourceID)
 out.RemainingSpans=intVal(missing["total"])
 pending:=getRow(db,`SELECT COUNT(DISTINCT p.id) AS total FROM propositions p
  JOIN proposition_provenance pr ON pr.proposition_id=p.id
  JOIN source_spans sp ON sp.id=pr.source_span_id
  WHERE sp.source_id=? AND p.status!='reviewed'`,sourceID)
 out.UnreviewedClaims=intVal(pending["total"])
 rows:=getRows(db,`SELECT DISTINCT p.id,p.statement,p.context,p.domain,p.created_at FROM propositions p
  JOIN proposition_provenance pr ON pr.proposition_id=p.id
  JOIN source_spans sp ON sp.id=pr.source_span_id
  WHERE sp.source_id=? AND p.status='reviewed'
  ORDER BY p.created_at,p.id LIMIT ?`,sourceID,limit+1)
 if len(rows)>limit {
  out.Blockers=append(out.Blockers,"Reviewed source claims exceed the chosen curriculum limit")
  rows=rows[:limit]
 }
 selected:=map[string]int{}
 for _,claim:=range rows {
  id:=strVal(claim["id"])
  evidence:=getRow(db,`SELECT sp.*,pr.quoted_text FROM proposition_provenance pr
   JOIN source_spans sp ON sp.id=pr.source_span_id
   JOIN extraction_batches b ON b.id=pr.batch_id
   WHERE pr.proposition_id=? AND sp.source_id=? AND b.status='accepted'
   ORDER BY sp.ordinal,sp.id LIMIT 1`,id,sourceID)
  if evidence==nil||!sourceSpanIntact(db,evidence)||
   !strings.Contains(strVal(evidence["content"]),strVal(evidence["quoted_text"])) {
   return SourceCurriculum{},errors.New("reviewed proposition lacks intact source evidence")
  }
  statement:=strVal(claim["statement"])
  step:=SourceCurriculumStep{
   PropositionID:id,ConceptID:sourceConceptID(id),
   Title:curriculumTitle(statement),Statement:statement,
   Context:strVal(claim["context"]),Domain:strVal(claim["domain"]),
   SourceSpanID:strVal(evidence["id"]),Quote:strVal(evidence["quoted_text"]),
   Dependencies:[]SourceCurriculumDependency{},
  }
  selected[id]=len(out.Steps)
  out.Steps=append(out.Steps,step)
 }
 // Retain only independently reviewed multi-premise chains with all premises
 // present. A missing external premise blocks a claim of curriculum readiness.
 chains:=getRows(db,"SELECT * FROM reasoning_chains WHERE status='reviewed' ORDER BY created_at,id LIMIT ?",1601)
 if len(chains)>1600{out.Blockers=append(out.Blockers,"Reasoning-chain review exceeds bounded curriculum capacity")}
 for _,chain:=range chains{
  target,exists:=selected[strVal(chain["conclusion_id"])]
  if !exists{continue}
  if len(chainReviewBlockers(db,chain))>0{
   out.Blockers=append(out.Blockers,"A reviewed chain has unresolved verification requirements")
   continue
  }
  premiseIDs:=parseList(chain["premises_json"])
  for _,premise:=range premiseIDs{
   idx,known:=selected[premise]
   if !known{
    out.Blockers=append(out.Blockers,"A reviewed reasoning chain depends on a claim outside the source curriculum")
    continue
   }
   link:=SourceCurriculumDependency{
    ConceptID:out.Steps[idx].ConceptID,
    ChainID:strVal(chain["id"]),
    Rationale:strVal(chain["explanation"]),
   }
   duplicate:=false
   for _,existing:=range out.Steps[target].Dependencies{
    if existing.ConceptID==link.ConceptID{duplicate=true;break}
   }
   if !duplicate{out.Steps[target].Dependencies=append(out.Steps[target].Dependencies,link)}
  }
 }
 for i:=range out.Steps {
  sort.Slice(out.Steps[i].Dependencies,func(a,b int)bool{
   return out.Steps[i].Dependencies[a].ConceptID<out.Steps[i].Dependencies[b].ConceptID
  })
 }
 // Kahn ordering keeps every prerequisite before its dependent. Stable source
 // order chooses between independent topics; cycles block publication.
 ids:=map[string]int{}
 for i,step:=range out.Steps{ids[step.ConceptID]=i}
 indegree:=make([]int,len(out.Steps))
 dependent:=make(map[int][]int)
 for i,step:=range out.Steps{
  for _,link:=range step.Dependencies{
   j,exists:=ids[link.ConceptID]
   if !exists{return SourceCurriculum{},errors.New("curriculum dependency identity mismatch")}
   indegree[i]++
   dependent[j]=append(dependent[j],i)
  }
 }
 ordered:=make([]SourceCurriculumStep,0,len(out.Steps))
 used:=make([]bool,len(out.Steps))
 for len(ordered)<len(out.Steps){
  found:=-1
  for i:=range out.Steps{
   if !used[i]&&indegree[i]==0{found=i;break}
  }
  if found<0{
   out.Blockers=append(out.Blockers,"Source reasoning dependencies contain a cycle")
   break
  }
  used[found]=true
  ordered=append(ordered,out.Steps[found])
  for _,next:=range dependent[found]{indegree[next]--}
 }
 if len(ordered)==len(out.Steps){out.Steps=ordered}
 if len(out.Steps)==0{out.Blockers=append(out.Blockers,"No reviewed source propositions are available")}
 if out.RemainingSpans>0{out.Blockers=append(out.Blockers,"Source extraction has uncovered spans")}
 if out.UnreviewedClaims>0{out.Blockers=append(out.Blockers,"Source propositions await independent review")}
 out.Ready=len(out.Blockers)==0
 raw,err:=json.Marshal(out)
 if err!=nil{return SourceCurriculum{},err}
 out.SHA256=fingerprint(string(raw))
 return out,nil
}

// PreviewSourceCurriculum constructs an evidence-bound route without writing
// concepts, lessons, questions, assessment or learner-state records.
func(s *Store)PreviewSourceCurriculum(sourceID string,limit int)(Value,error){
 return s.with(func(db *database)Value{
  plan,err:=curriculumSnapshot(db,sourceID,limit)
  if err!=nil{panic(err)}
  return Value{"curriculum":plan,"mastery_awarded":false}
 })
}

// ImportSourceCurriculum creates only the reviewable concept/prerequisite graph.
// A separate session must gather actual learner responses and evaluations.
func(s *Store)ImportSourceCurriculum(sourceID string,limit int,expectedSHA256 string,confirm bool)(Value,error){
 return s.with(func(db *database)Value{
  insist(confirm&&len(expectedSHA256)==64,"curriculum import requires confirmed evidence digest")
  plan,err:=curriculumSnapshot(db,sourceID,limit)
  if err!=nil{panic(err)}
  insist(plan.Ready && plan.SHA256==expectedSHA256,"source curriculum changed or requires further review")
  created,linked:=0,0
  for _,step:=range plan.Steps{
   existing:=getRow(db,"SELECT title,summary,domain FROM concepts WHERE id=?",step.ConceptID)
   if existing!=nil{
    insist(strVal(existing["title"])==step.Title &&
     strVal(existing["summary"])==step.Statement &&
     strVal(existing["domain"])==step.Domain,
     "curriculum concept identity conflicts with an existing record")
   }else{
    exec(db,"INSERT INTO concepts(id,title,summary,domain,created_at) VALUES(?,?,?,?,?)",
     step.ConceptID,step.Title,step.Statement,step.Domain,now())
    created++
   }
  }
  for _,step:=range plan.Steps{
   for _,dependency:=range step.Dependencies{
    prior:=getRow(db,"SELECT rationale FROM relations WHERE source=? AND target=? AND kind='requires'",
     step.ConceptID,dependency.ConceptID)
    if prior!=nil{continue}
    exec(db,"INSERT INTO relations(source,target,kind,rationale) VALUES(?,?,'requires',?)",
     step.ConceptID,dependency.ConceptID,
     "Reviewed source reasoning chain "+dependency.ChainID+": "+dependency.Rationale)
    linked++
   }
  }
  return Value{
   "source_id":sourceID,"sha256":plan.SHA256,"concepts_created":created,
   "prerequisites_linked":linked,"concept_ids":curriculumConceptIDs(plan.Steps),
   "mastery_awarded":false,
  }
 })
}

func curriculumConceptIDs(steps []SourceCurriculumStep)[]string{
 ids:=make([]string,0,len(steps))
 for _,step:=range steps{ids=append(ids,step.ConceptID)}
 return ids
}
