package controller

import (
 "errors"
 "fmt"
 "strings"
 "unicode/utf8"
)

func validClaimKind(kind string)bool{
 return kind=="hypothesis"||kind=="assertion"||kind=="question"
}
func validEvidenceRelation(relation string)bool{
 return relation=="supports"||relation=="challenges"||relation=="context"
}

// Research claims record an owner's hypotheses and source relations. Evidence
// counts describe documentation, not the truth of a proposition.
func (db *Database) CreateResearchClaim(project,statement,kind string)(map[string]any,error){
 statement=strings.TrimSpace(statement)
 if !validID.MatchString(project)||!validClaimKind(kind)||len(statement)<3||len(statement)>1200||
  !utf8.ValidString(statement)||strings.ContainsAny(statement,"\x00"){
  return nil,errors.New("invalid research claim")
 }
 id,now:=randomHex(16),stamp()
 if err:=db.transaction(func()error{
  if err:=requireProject(db,project);err!=nil{return err}
  return db.execLocked("INSERT INTO research_claims(id,project,statement,kind,revision,created,updated) VALUES(?,?,?,?,1,?,?)",
   id,project,statement,kind,now,now)
 });err!=nil{return nil,err}
 return map[string]any{"id":id,"project_id":project,"statement":statement,"kind":kind,
  "revision":1,"created":now,"assessment":"unassessed"},nil
}

func (db *Database) ResearchClaims(project string)([]map[string]any,error){
 if !validID.MatchString(project){return nil,errors.New("invalid project scope")}
 return db.query(`SELECT c.id,c.project AS project_id,c.statement,c.kind,c.revision,
   c.created,c.updated,count(e.passage) AS evidence_links
   FROM research_claims c LEFT JOIN research_claim_evidence e ON e.claim=c.id
   WHERE c.project=? GROUP BY c.id ORDER BY c.updated DESC,c.id LIMIT 100`,project)
}

func (db *Database) AddResearchClaimEvidence(project,claim,passage,relation string,revision int)(map[string]any,error){
 if !validID.MatchString(project)||!contributionArtifactID.MatchString(claim)||
  !contributionArtifactID.MatchString(passage)||!validEvidenceRelation(relation)||revision<1{
  return nil,errors.New("invalid evidence mutation")
 }
 var updated int
 err:=db.transaction(func()error{
  row,err:=db.lockedOne("SELECT revision FROM research_claims WHERE id=? AND project=?",claim,project)
  if err!=nil{return err}
  if row==nil{return errors.New("research claim is unavailable for this project")}
  previous,err:=db.lockedOne("SELECT relation FROM research_claim_evidence WHERE claim=? AND passage=?",claim,passage)
  if err!=nil{return err}
  if previous!=nil{
   if asString(previous["relation"])!=relation{return errors.New("evidence relation conflict")}
   updated=asInt(row["revision"])
   return nil
  }
  if asInt(row["revision"])!=revision{return errors.New("claim revision conflict")}
  source,err:=db.lockedOne(`SELECT p.body,p.body_sha256 FROM research_passages p
   JOIN research_sources s ON s.id=p.source AND s.epoch=p.epoch
   JOIN project_libraries l ON l.library=s.library AND l.project=?
   WHERE p.id=?`,project,passage)
  if err!=nil{return err}
  if source==nil||checksum([]byte(asString(source["body"])))!=asString(source["body_sha256"]){
   return errors.New("linked passage is unavailable or not verified")
  }
  now:=stamp()
  if err:=db.execLocked("INSERT INTO research_claim_evidence(claim,passage,relation,body_sha256,created) VALUES(?,?,?,?,?)",
   claim,passage,relation,asString(source["body_sha256"]),now);err!=nil{return err}
  updated=revision+1
  return db.execLocked("UPDATE research_claims SET revision=?,updated=? WHERE id=? AND project=?",
   updated,now,claim,project)
 })
 if err!=nil{return nil,err}
 return map[string]any{"claim_id":claim,"passage_id":passage,"relation":relation,
  "revision":updated},nil
}

func (db *Database) ReadResearchClaim(project,claim string)(map[string]any,error){
 if !validID.MatchString(project)||!contributionArtifactID.MatchString(claim){
  return nil,errors.New("invalid research claim scope")
 }
 result,err:=db.queryOne(`SELECT id,project AS project_id,statement,kind,revision,created,updated
   FROM research_claims WHERE project=? AND id=?`,project,claim)
 if err!=nil{return nil,err}
 if result==nil{return nil,errors.New("research claim not found")}
 rows,err:=db.query(`SELECT e.passage,e.relation,e.body_sha256 AS expected_digest,
    p.body_sha256 AS observed_digest,p.source,p.ordinal,p.epoch,s.epoch AS source_epoch,
    CASE WHEN l.project IS NOT NULL THEN p.body ELSE '' END AS visible_body,
    CASE WHEN l.project IS NOT NULL THEN s.title ELSE '' END AS visible_title,
    CASE WHEN l.project IS NOT NULL THEN 1 ELSE 0 END AS authorized
   FROM research_claim_evidence e
   LEFT JOIN research_passages p ON p.id=e.passage
   LEFT JOIN research_sources s ON s.id=p.source
   LEFT JOIN project_libraries l ON l.library=s.library AND l.project=?
   WHERE e.claim=? ORDER BY e.created,e.passage LIMIT 100`,project,claim)
 if err!=nil{return nil,err}
 evidence:=make([]map[string]any,0,len(rows))
 support,challenge,unavailable:=0,0,0
 for _,row:=range rows{
  ok:=asInt(row["authorized"])==1&&asInt(row["epoch"])==asInt(row["source_epoch"])&&
   asString(row["expected_digest"])==asString(row["observed_digest"])&&
   checksum([]byte(asString(row["visible_body"])))==asString(row["expected_digest"])
  item:=map[string]any{"passage_id":asString(row["passage"]),"relation":asString(row["relation"]),"active":ok}
  if ok {
   excerpt:=asString(row["visible_body"])
   if len(excerpt)>500{excerpt=excerpt[:500]}
   item["excerpt"]=excerpt
   item["source_title"]=row["visible_title"]
   item["citation_ref"]=fmt.Sprintf("source:%s@%d:%d",
    asString(row["source"]),asInt(row["epoch"]),asInt(row["ordinal"]))
   switch asString(row["relation"]){case "supports":support++;case "challenges":challenge++}
  }else{unavailable++}
  evidence=append(evidence,item)
 }
 assessment:="unassessed"
 switch{
 case support>0&&challenge>0:assessment="contested_evidence"
 case support>0:assessment="supporting_evidence"
 case challenge>0:assessment="challenging_evidence"
 }
 result["assessment"]=assessment
 result["supporting_sources"]=support
 result["challenging_sources"]=challenge
 result["unavailable_links"]=unavailable
 result["evidence"]=evidence
 return result,nil
}
