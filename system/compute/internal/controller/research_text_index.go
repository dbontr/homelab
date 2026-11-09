package controller

import (
 "context"
 "errors"
 "io"
 "strings"
 "time"
 "unicode/utf8"
)

const maxIndexedTextBytes = 96 << 10
const indexedTextPassageBytes = 2048

type pendingTextObject struct {
 Library string
 Name string
 Digest string
 Revision int
}

// Text ingestion requires separate owner consent from model embeddings.
// Both use the original project's shared-library storage and source index.
func (db *Database) SetTextIndexPolicy(library string,enabled bool,revision int)(map[string]any,error){
 if !validID.MatchString(library)||revision<0{return nil,errors.New("invalid text index policy")}
 next:=revision+1
 err:=db.transaction(func()error{
  exists,err:=db.lockedOne("SELECT id FROM libraries WHERE id=?",library)
  if err!=nil{return err}
  if exists==nil{return errors.New("library not found")}
  current,err:=db.lockedOne("SELECT revision FROM research_text_policies WHERE library=?",library)
  if err!=nil{return err}
  if current==nil{
   if revision!=0{return errors.New("text indexing policy revision conflict")}
   return db.execLocked("INSERT INTO research_text_policies(library,enabled,revision,updated) VALUES(?,?,?,?)",
    library,boolInt(enabled),next,stamp())
  }
  if asInt(current["revision"])!=revision{return errors.New("text indexing policy revision conflict")}
  return db.execLocked("UPDATE research_text_policies SET enabled=?,revision=?,updated=? WHERE library=?",
   boolInt(enabled),next,stamp(),library)
 })
 if err!=nil{return nil,err}
 return map[string]any{"library":library,"enabled":enabled,"revision":next},nil
}
func (db *Database) ProjectTextIndexPolicy(project,library string)(map[string]any,error){
 if !validID.MatchString(project)||!validID.MatchString(library){return nil,errors.New("invalid policy scope")}
 row,err:=db.queryOne(`SELECT l.library,p.enabled,p.revision,p.updated FROM project_libraries l
   LEFT JOIN research_text_policies p ON p.library=l.library
   WHERE l.project=? AND l.library=?`,project,library)
 if err!=nil{return nil,err}
 if row==nil{return nil,errors.New("library not linked to project")}
 if row["revision"]==nil{return map[string]any{"library":library,"enabled":false,"revision":0},nil}
 row["enabled"]=asInt(row["enabled"])==1
 return row,nil
}

func (db *Database) pendingTextObjects(limit int)([]pendingTextObject,error){
 rows,err:=db.query(`SELECT o.library,o.name,o.sha256,p.revision FROM library_objects o
 JOIN research_text_policies p ON p.library=o.library AND p.enabled=1
 WHERE o.size>0 AND o.size<=? AND (
   lower(o.name) LIKE '%.txt' OR lower(o.name) LIKE '%.md' OR lower(o.name) LIKE '%.json')
 AND NOT EXISTS(
   SELECT 1 FROM research_sources s
   WHERE s.library=o.library AND s.origin_kind='object'
     AND s.origin_name=o.name AND s.origin_digest=o.sha256 AND s.index_revision>0)
 AND NOT EXISTS(
   SELECT 1 FROM research_text_failures f WHERE f.library=o.library AND f.name=o.name
     AND f.digest=o.sha256 AND f.policy_revision=p.revision)
 ORDER BY o.created,o.library,o.name LIMIT ?`,maxIndexedTextBytes,limit)
 if err!=nil{return nil,err}
 pending:=make([]pendingTextObject,0,len(rows))
 for _,row:=range rows{
  pending=append(pending,pendingTextObject{Library:asString(row["library"]),
   Name:asString(row["name"]),Digest:asString(row["sha256"]),Revision:asInt(row["revision"])})
 }
 return pending,nil
}

func splitIndexedText(text string)([]ResearchPassageInput,error){
 if len(text)==0||len(text)>maxIndexedTextBytes||!utf8.ValidString(text)||
  strings.ContainsRune(text,0)||strings.TrimSpace(text)==""{
  return nil,errors.New("source must contain nonempty UTF-8 text within indexing bounds")
 }
 result:=make([]ResearchPassageInput,0,64)
 var body strings.Builder
 line,start:=1,1
 flush:=func(){
  if body.Len()==0{return}
  result=append(result,ResearchPassageInput{Ordinal:len(result)+1,
   Text:body.String(),LineStart:start,LineEnd:line})
  body.Reset()
  start=line
 }
 for _,r:=range text {
  if body.Len()+utf8.RuneLen(r)>indexedTextPassageBytes{flush()}
  body.WriteRune(r)
  if r=='\n'{line++}
 }
 flush()
 if len(result)==0||len(result)>64{return nil,errors.New("source exceeds 64 indexed passages")}
 return result,nil
}

func (db *Database) ensureTextSource(item pendingTextObject)(string,bool,error){
 id:=randomHex(16)
 exists:=false
 err:=db.transaction(func()error{
  policy,err:=db.lockedOne("SELECT revision,enabled FROM research_text_policies WHERE library=?",item.Library)
  if err!=nil{return err}
  if policy==nil||asInt(policy["enabled"])!=1||asInt(policy["revision"])!=item.Revision{
   return errors.New("text indexing permission changed")
  }
  obj,err:=db.lockedOne("SELECT sha256 FROM library_objects WHERE library=? AND name=?",item.Library,item.Name)
  if err!=nil{return err}
  if obj==nil||asString(obj["sha256"])!=item.Digest{return errors.New("source object changed")}
  prior,err:=db.lockedOne(`SELECT id,index_revision FROM research_sources
    WHERE library=? AND origin_kind='object' AND origin_name=? AND origin_digest=?
    ORDER BY index_revision DESC,created DESC LIMIT 1`,item.Library,item.Name,item.Digest)
  if err!=nil{return err}
  if prior!=nil{
   id=asString(prior["id"])
   exists=asInt(prior["index_revision"])>0
   return nil
  }
  now:=stamp()
  title:=item.Name
  if len(title)>200{title=title[:200]}
  return db.execLocked(`INSERT INTO research_sources
    (id,library,title,origin_kind,origin_name,origin_revision,origin_digest,
     origin_path,epoch,index_revision,created,updated)
    VALUES(?,?,?,?,?,?,?,?,1,0,?,?)`,
   id,item.Library,title,"object",item.Name,"",item.Digest,"",now,now)
 })
 return id,exists,err
}

func (db *Database) markTextFailure(item pendingTextObject,reason string)error{
 if len(reason)>160{reason=reason[:160]}
 return db.transaction(func()error{
  policy,err:=db.lockedOne("SELECT revision FROM research_text_policies WHERE library=?",item.Library)
  if err!=nil{return err}
  if policy==nil||asInt(policy["revision"])!=item.Revision{return nil}
  return db.execLocked(`INSERT INTO research_text_failures
    (library,name,digest,policy_revision,reason,updated) VALUES(?,?,?,?,?,?)
    ON CONFLICT(library,name,digest) DO UPDATE SET
    policy_revision=excluded.policy_revision,reason=excluded.reason,updated=excluded.updated`,
   item.Library,item.Name,item.Digest,item.Revision,reason,stamp())
 })
}

func (db *Database) TextIndexFailures(library string)([]map[string]any,error){
 if !validID.MatchString(library){return nil,errors.New("invalid library")}
 return db.query(`SELECT name,digest,policy_revision,reason,updated
   FROM research_text_failures WHERE library=? ORDER BY updated DESC LIMIT 100`,library)
}

func (db *Database) indexTextObject(ctx context.Context,item pendingTextObject)error{
 if err:=ctx.Err();err!=nil{return err}
 file,meta,err:=db.OpenLibraryObject(item.Library,item.Name)
 if err!=nil{return err}
 defer file.Close()
 if asString(meta["sha256"])!=item.Digest{return errors.New("source digest changed")}
 bytes,err:=io.ReadAll(io.LimitReader(file,maxIndexedTextBytes+1))
 if err!=nil{return err}
 if len(bytes)>maxIndexedTextBytes||checksum(bytes)!=item.Digest{
  return errors.New("stored source exceeded bound or checksum differs")
 }
 passages,err:=splitIndexedText(string(bytes))
 if err!=nil{return err}
 id,already,err:=db.ensureTextSource(item)
 if err!=nil{return err}
 if already{return nil}
 _,err=db.ingestConsentedText(item.Library,id,passages)
 if err!=nil{
  // A peer may have finished indexing the same source between transactions.
  rows,e:=db.query("SELECT index_revision FROM research_sources WHERE id=?",id)
  if e==nil&&len(rows)==1&&asInt(rows[0]["index_revision"])>0{return nil}
 }
 return err
}

func (s *Server) IndexPendingText(ctx context.Context,limit int)(map[string]any,error){
 if limit<1||limit>8{return nil,errors.New("text indexing batch must be 1-8")}
 pending,err:=s.DB.pendingTextObjects(limit)
 if err!=nil{return nil,err}
 indexed,failed:=0,0
 for _,item:=range pending{
  if err:=ctx.Err();err!=nil{return nil,err}
  if err:=s.DB.indexTextObject(ctx,item);err!=nil{
   if err:=s.DB.markTextFailure(item,"text indexing failed: "+err.Error());err!=nil{return nil,err}
   failed++
  }else{indexed++}
 }
 return map[string]any{"indexed":indexed,"failed":failed,"considered":len(pending)},nil
}

func (s *Server) StartResearchTextWorker(ctx context.Context){
 go func(){
  timer:=time.NewTimer(0)
  defer timer.Stop()
  for{
   select{
   case <-ctx.Done():return
   case <-timer.C:
    budget,cancel:=context.WithTimeout(ctx,30*time.Second)
    _,_ = s.IndexPendingText(budget,4)
    cancel()
    timer.Reset(time.Minute)
   }
  }
 }()
}
