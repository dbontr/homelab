package controller

import (
 "context"
 "errors"
 "time"
)

type pendingEmbedding struct{
 Library string
 Passage string
 Body string
 Digest string
}

// SetEmbeddingPolicy requires the controller owner's authority. Consent is
// per library, not inherited from repository access or project membership.
func (db *Database) SetEmbeddingPolicy(library,model string,enabled bool,revision int)(map[string]any,error){
 if !validID.MatchString(library)||!embeddingModelID.MatchString(model)||revision<0{
  return nil,errors.New("invalid embedding permission")
 }
 next:=revision+1
 err:=db.transaction(func()error{
  source,err:=db.lockedOne("SELECT id FROM libraries WHERE id=?",library)
  if err!=nil{return err}
  if source==nil{return errors.New("research library does not exist")}
  previous,err:=db.lockedOne("SELECT revision FROM research_embedding_policies WHERE library=?",library)
  if err!=nil{return err}
  if previous==nil{
   if revision!=0{return errors.New("embedding permission revision conflict")}
   return db.execLocked("INSERT INTO research_embedding_policies(library,model,enabled,revision,updated) VALUES(?,?,?,?,?)",
     library,model,boolInt(enabled),next,stamp())
  }
  if asInt(previous["revision"])!=revision{return errors.New("embedding permission revision conflict")}
  return db.execLocked("UPDATE research_embedding_policies SET model=?,enabled=?,revision=?,updated=? WHERE library=?",
    model,boolInt(enabled),next,stamp(),library)
 })
 if err!=nil{return nil,err}
 return map[string]any{"library":library,"model":model,"enabled":enabled,"revision":next},nil
}

func boolInt(value bool)int{if value{return 1};return 0}

// Revocation remains available when the model provider is offline or removed.
func (db *Database) DisableEmbeddingPolicy(library string,revision int)(map[string]any,error){
 if !validID.MatchString(library)||revision<1{return nil,errors.New("invalid embedding permission")}
 rows,err:=db.query("SELECT model FROM research_embedding_policies WHERE library=?",library)
 if err!=nil{return nil,err}
 if len(rows)!=1{return nil,errors.New("embedding permission not found")}
 return db.SetEmbeddingPolicy(library,asString(rows[0]["model"]),false,revision)
}

func (db *Database) ProjectEmbeddingPolicy(project,library string)(map[string]any,error){
 if !validID.MatchString(project)||!validID.MatchString(library){
  return nil,errors.New("invalid embedding policy scope")
 }
 rows,err:=db.query(\`SELECT p.library,p.model,p.enabled,p.revision,p.updated
   FROM research_embedding_policies p
   JOIN project_libraries l ON l.library=p.library
   WHERE l.project=? AND p.library=?\`,project,library)
 if err!=nil{return nil,err}
 if len(rows)==0{return nil,errors.New("embedding permission unavailable for this project")}
 return rows[0],nil
}

func (db *Database) enabledEmbeddingLibraries(project,model string)(int,error){
 if !validID.MatchString(project)||!embeddingModelID.MatchString(model){
  return 0,errors.New("invalid embedding search scope")
 }
 rows,err:=db.query(\`SELECT p.library FROM research_embedding_policies p
   JOIN project_libraries l ON l.library=p.library
   WHERE l.project=? AND p.model=? AND p.enabled=1 LIMIT 1\`,project,model)
 if err!=nil{return 0,err}
 return len(rows),nil
}

func (db *Database) pendingResearchEmbeddings(model string,limit int)([]pendingEmbedding,error){
 if !embeddingModelID.MatchString(model)||limit<1||limit>16{return nil,errors.New("invalid embedding batch")}
 rows,err:=db.query(\`SELECT s.library,p.id,p.body,p.body_sha256
  FROM research_passages p
  JOIN research_sources s ON s.id=p.source AND p.epoch=s.epoch
  JOIN research_embedding_policies policy ON policy.library=s.library AND policy.enabled=1 AND policy.model=?
  LEFT JOIN research_embeddings e ON e.passage=p.id AND e.model=policy.model
  WHERE e.passage IS NULL ORDER BY p.created,p.id LIMIT ?\`,model,limit)
 if err!=nil{return nil,err}
 result:=make([]pendingEmbedding,0,len(rows))
 for _,row:=range rows{
  result=append(result,pendingEmbedding{
   Library:asString(row["library"]),Passage:asString(row["id"]),
   Body:asString(row["body"]),Digest:asString(row["body_sha256"]),
  })
 }
 return result,nil
}

// putConsentedEmbedding checks the permission inside the same database
// transaction as the immutable vector write, fencing revocation.
func (db *Database) putConsentedEmbedding(library,passage,model,digest string,vector []float64) error {
 _,err:=db.putResearchEmbedding(library,passage,model,digest,vector,true)
 return err
}

func (s *Server) IndexPendingResearch(ctx context.Context,limit int)(map[string]any,error){
 if s.Embedder==nil{return nil,errors.New("embedding provider is not configured")}
 if limit<1||limit>16{return nil,errors.New("invalid embedding batch limit")}
 pending,err:=s.DB.pendingResearchEmbeddings(s.Embedder.Model,limit)
 if err!=nil{return nil,err}
 count:=0
 for _,candidate:=range pending{
  if err:=ctx.Err();err!=nil{return nil,err}
  if checksum([]byte(candidate.Body))!=candidate.Digest{
   return nil,errors.New("stored research source checksum mismatch")
  }
  vector,err:=s.Embedder.Embed(ctx,candidate.Body)
  if err!=nil{return nil,err}
  if err:=s.DB.putConsentedEmbedding(candidate.Library,candidate.Passage,s.Embedder.Model,candidate.Digest,vector);err!=nil{
   return nil,err
  }
  count++
 }
 return map[string]any{"embedded":count,"model":s.Embedder.Model},nil
}

func (s *Server) SemanticResearchQuery(ctx context.Context,project,query string,limit int)([]map[string]any,error){
 if s.Embedder==nil{return nil,errors.New("embedding provider is not configured")}
 if len(query)<1||len(query)>1000||limit<1||limit>100{
  return nil,errors.New("invalid semantic research request")
 }
 permitted,err:=s.DB.enabledEmbeddingLibraries(project,s.Embedder.Model)
 if err!=nil{return nil,err}
 if permitted==0{return nil,errors.New("semantic search requires enabled library consent")}
 vector,err:=s.Embedder.Embed(ctx,query)
 if err!=nil{return nil,err}
 return s.DB.SemanticResearch(project,s.Embedder.Model,vector,limit)
}

// Automatic indexing sends only explicitly consented source passages. Failures
// leave the original research index intact; the next cycle retries.
func (s *Server) StartResearchEmbeddingWorker(ctx context.Context){
 if s.Embedder==nil{return}
 go func(){
  timer:=time.NewTimer(0)
  defer timer.Stop()
  for {
   select {
   case <-ctx.Done():return
   case <-timer.C:
    batch,cancel:=context.WithTimeout(ctx,90*time.Second)
    _,_ = s.IndexPendingResearch(batch,4)
    cancel()
    timer.Reset(time.Minute)
   }
  }
 }()
}
