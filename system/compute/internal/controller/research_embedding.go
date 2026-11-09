package controller

import (
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "regexp"
 "sort"
)

var embeddingModelID=regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._/-]{0,95}$")

const (
 minEmbeddingDimensions=8
 maxEmbeddingDimensions=1536
 maxSemanticCandidates=2000
)

func vectorNorm(values []float64)(float64,error){
 if len(values)<minEmbeddingDimensions||len(values)>maxEmbeddingDimensions{
  return 0,errors.New("embedding dimension outside permitted range")
 }
 magnitude:=0.0
 for _,value:=range values{
  if math.IsNaN(value)||math.IsInf(value,0)||math.Abs(value)>1e6{
   return 0,errors.New("non-finite or excessive embedding component")
  }
  magnitude+=value*value
 }
 if magnitude<1e-16||math.IsInf(magnitude,0){
  return 0,errors.New("embedding has no usable magnitude")
 }
 return math.Sqrt(magnitude),nil
}

// PutResearchEmbedding attaches a model-specific vector to a fixed passage
// and checksum. Replacement with a different vector is rejected; a changed
// passage must use an independent passage ID and a new index epoch.
func (db *Database) PutResearchEmbedding(library,passage,model,digest string,vector []float64)(map[string]any,error){
 return db.putResearchEmbedding(library,passage,model,digest,vector,false)
}
func (db *Database) putResearchEmbedding(library,passage,model,digest string,vector []float64,requireConsent bool)(map[string]any,error){
 if !validID.MatchString(library)||!contributionArtifactID.MatchString(passage)||
   !embeddingModelID.MatchString(model)||!shaPattern.MatchString(digest){
  return nil,errors.New("invalid embedding provenance")
 }
 if _,err:=vectorNorm(vector);err!=nil{return nil,err}
 raw,err:=json.Marshal(vector)
 if err!=nil||len(raw)>32<<10{return nil,errors.New("embedding exceeds storage limit")}
 result:=map[string]any{"passage_id":passage,"library":library,"model":model,"dimension":len(vector),"body_sha256":digest}
 err=db.transaction(func()error{
  row,err:=db.lockedOne(`SELECT p.body_sha256,p.body,p.epoch,s.library,s.epoch AS current_epoch
    FROM research_passages p JOIN research_sources s ON s.id=p.source
    WHERE p.id=? AND s.library=?`,passage,library)
  if err!=nil{return err}
  if row==nil||asString(row["body_sha256"])!=digest||checksum([]byte(asString(row["body"])))!=digest{
   return errors.New("source passage checksum mismatch")
  }
  if requireConsent{
   policy,err:=db.lockedOne("SELECT enabled FROM research_embedding_policies WHERE library=? AND model=?",library,model)
   if err!=nil{return err}
   if policy==nil||asInt(policy["enabled"])!=1{return errors.New("embedding permission revoked")}
  }
  if asInt(row["current_epoch"])!=asInt(row["epoch"]){return errors.New("source passage was superseded")}
  previous,err:=db.lockedOne("SELECT vector,body_sha256 FROM research_embeddings WHERE passage=? AND model=?",passage,model)
  if err!=nil{return err}
  if previous!=nil{
   if asString(previous["body_sha256"])!=digest||asString(previous["vector"])!=string(raw){
    return errors.New("immutable embedding already exists for passage and model")
   }
   return nil
  }
  return db.execLocked("INSERT INTO research_embeddings(passage,model,dimension,vector,body_sha256,epoch,created) VALUES(?,?,?,?,?,?,?)",
    passage,model,len(vector),string(raw),digest,asInt(row["epoch"]),stamp())
 })
 return result,err
}

// SemanticResearch retrieves only passages in project-linked libraries and
// the source's active index epoch. The caller supplies the query vector from
// the exact same encoder model. This method never invents embeddings.
func (db *Database) SemanticResearch(project,model string,query []float64,limit int)([]map[string]any,error){
 return db.semanticResearch(project,model,query,limit,false)
}

func (db *Database) semanticResearch(project,model string,query []float64,limit int,consentedOnly bool)([]map[string]any,error){
 if !validID.MatchString(project)||!embeddingModelID.MatchString(model)||limit<1||limit>100{
  return nil,errors.New("invalid semantic research scope")
 }
 queryNorm,err:=vectorNorm(query)
 if err!=nil{return nil,err}
 rows,err:=db.query(`SELECT p.id,p.source,s.library,s.title,s.origin_kind,
    s.origin_name,s.origin_revision,s.origin_digest,s.origin_path,p.epoch,
    p.ordinal,p.page,p.section,p.line_start,p.line_end,p.body_sha256,p.body,
    substr(p.body,1,1200) AS excerpt,e.vector,e.dimension,e.body_sha256 AS vector_digest
    FROM research_embeddings e
    JOIN research_passages p ON p.id=e.passage AND p.epoch=e.epoch
    JOIN research_sources s ON s.id=p.source AND s.epoch=p.epoch
    JOIN project_libraries l ON l.library=s.library
    WHERE l.project=? AND e.model=? AND e.dimension=? AND e.body_sha256=p.body_sha256
    AND (?=0 OR EXISTS(SELECT 1 FROM research_embedding_policies permission
        WHERE permission.library=s.library AND permission.model=e.model AND permission.enabled=1))
    ORDER BY p.id LIMIT ?`,project,model,len(query),boolInt(consentedOnly),maxSemanticCandidates+1)
 if err!=nil{return nil,err}
 if len(rows)>maxSemanticCandidates{return nil,errors.New("semantic index exceeds bounded scan capacity")}
 result:=make([]map[string]any,0,len(rows))
 for _,row:=range rows{
  if checksum([]byte(asString(row["body"])))!=asString(row["body_sha256"]){
   return nil,errors.New("stored passage checksum mismatch")
  }
  delete(row,"body")
  var candidate []float64
  if err:=json.Unmarshal([]byte(asString(row["vector"])),&candidate);err!=nil{
   return nil,errors.New("stored embedding cannot be decoded")
  }
  candidateNorm,err:=vectorNorm(candidate)
  if err!=nil||len(candidate)!=len(query){return nil,errors.New("stored embedding has invalid dimension")}
  dot:=0.0
  for i,value:=range query{dot+=value*candidate[i]}
  similarity:=dot/(queryNorm*candidateNorm)
  if math.IsNaN(similarity)||math.IsInf(similarity,0){return nil,errors.New("invalid semantic similarity")}
  if similarity>1{similarity=1};if similarity< -1{similarity= -1}
  delete(row,"vector")
  delete(row,"vector_digest")
  delete(row,"dimension")
  row["score"]=similarity
  row["embedding_model"]=model
  row["citation_ref"]=fmt.Sprintf("source:%s@%d:%d",asString(row["source"]),asInt(row["epoch"]),asInt(row["ordinal"]))
  result=append(result,row)
 }
 sort.Slice(result,func(i,j int)bool{
  x,y:=result[i]["score"].(float64),result[j]["score"].(float64)
  if x!=y{return x>y}
  return asString(result[i]["id"])<asString(result[j]["id"])
 })
 if len(result)>limit{result=result[:limit]}
 return result,nil
}
