// Package health persists health observations and versioned plans.
package health

import (
 "context"
 "crypto/rand"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "path/filepath"
 "os"
 "regexp"
 "strings"
 "time"

 _ "modernc.org/sqlite"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var ErrConflict = errors.New("record or plan revision conflicts with stored data")

type Store struct { db *sql.DB }

type Record struct {
 ID string `json:"id"`
 Domain string `json:"domain"`
 Metric string `json:"metric"`
 Value float64 `json:"value"`
 Unit string `json:"unit"`
 At time.Time `json:"at"`
 Source string `json:"source"`
 SourceID string `json:"source_id,omitempty"`
 Method string `json:"method"`
 Confidence *float64 `json:"confidence,omitempty"`
 Metadata json.RawMessage `json:"metadata,omitempty"`
 Replaces string `json:"replaces,omitempty"`
 CreatedAt time.Time `json:"created_at"`
}

type Session struct {
 ID string `json:"id"`
 Date string `json:"date"`
 Title string `json:"title"`
 Exercises []Exercise `json:"exercises,omitempty"`
 Items []MealItem `json:"items,omitempty"`
}
type Exercise struct {
 Name string `json:"name"`
 Sets int `json:"sets,omitempty"`
 Repetitions int `json:"repetitions,omitempty"`
 Target string `json:"target,omitempty"`
}
type Plan struct {
 ID string `json:"id"`
 Kind string `json:"kind"`
 Title string `json:"title"`
 Sessions []Session `json:"sessions"`
 Revision int `json:"revision"`
 UpdatedAt time.Time `json:"updated_at"`
}
type PendingSession struct {
 PlanID string `json:"plan_id"`
 Session Session `json:"session"`
 State string `json:"state"`
}

func newID() (string,error) {
 b:=make([]byte,16)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return hex.EncodeToString(b),nil
}
func Open(path string) (*Store,error) {
 if strings.TrimSpace(path)=="" {return nil,errors.New("database path is required")}
 if path!=":memory:" {
   dir:=filepath.Dir(path)
   if err:=os.MkdirAll(dir,0700);err!=nil{return nil,fmt.Errorf("database directory: %w",err)}
   info,err:=os.Stat(dir)
   if err!=nil{return nil,err}
   if !info.IsDir()||info.Mode().Perm()&0077!=0{
      return nil,errors.New("database directory must be private (permissions 0700)")
   }
   f,err:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_RDWR,0600)
   if err==nil {_=f.Close()} else if !os.IsExist(err) {return nil,fmt.Errorf("create database: %w",err)}
   info,err=os.Stat(path)
   if err!=nil{return nil,err}
   if info.Mode().Perm()&0077!=0 {return nil,errors.New("database file must not be accessible to group or others")}
 }
 db,err:=sql.Open("sqlite",path)
 if err!=nil{return nil,err}
 db.SetMaxOpenConns(1)
 for _,stmt:=range []string{
  "PRAGMA foreign_keys=ON",
  "PRAGMA journal_mode=WAL",
  "PRAGMA busy_timeout=5000",
  `CREATE TABLE IF NOT EXISTS records (
      id TEXT PRIMARY KEY, domain TEXT NOT NULL, metric TEXT NOT NULL,
      value REAL NOT NULL, unit TEXT NOT NULL, at TEXT NOT NULL,
      source TEXT NOT NULL, source_id TEXT, method TEXT NOT NULL,
      confidence REAL, metadata TEXT NOT NULL DEFAULT '{}',
      replaces TEXT UNIQUE REFERENCES records(id), created_at TEXT NOT NULL,
      UNIQUE(source, source_id)
  )`,
  "CREATE INDEX IF NOT EXISTS idx_records_lookup ON records(domain,metric,at)",
  `CREATE TABLE IF NOT EXISTS plans(
      id TEXT PRIMARY KEY, kind TEXT NOT NULL, title TEXT NOT NULL,
      sessions TEXT NOT NULL, revision INTEGER NOT NULL, updated_at TEXT NOT NULL
  )`,
  `CREATE TABLE IF NOT EXISTS plan_history(
      plan_id TEXT NOT NULL, revision INTEGER NOT NULL, document TEXT NOT NULL,
      changed_at TEXT NOT NULL, PRIMARY KEY(plan_id,revision),
      FOREIGN KEY(plan_id) REFERENCES plans(id)
  )`,
  `CREATE TABLE IF NOT EXISTS session_decisions(
      plan_id TEXT NOT NULL, session_id TEXT NOT NULL, state TEXT NOT NULL,
      scheduled_date TEXT NOT NULL, decided_at TEXT NOT NULL, PRIMARY KEY(plan_id,session_id),
      FOREIGN KEY(plan_id) REFERENCES plans(id)
  )`,
  `CREATE TABLE IF NOT EXISTS goals(
      metric TEXT PRIMARY KEY, value REAL NOT NULL, unit TEXT NOT NULL,
      updated_at TEXT NOT NULL
  )`,
 } {
  if _,err=db.Exec(stmt);err!=nil {_=db.Close();return nil,fmt.Errorf("database setup: %w",err)}
 }
 s := &Store{db:db}
 if err:=s.initJournal();err!=nil{_ =db.Close();return nil,err}
 if err:=s.initTraining();err!=nil{_ =db.Close();return nil,err}
 if err:=s.initGroceries();err!=nil{_ =db.Close();return nil,err}
 if err:=s.initEvidence();err!=nil{_ =db.Close();return nil,err}
 if err:=s.initMedications();err!=nil{_ =db.Close();return nil,err}
 if err:=s.initFoodProposals();err!=nil{_ =db.Close();return nil,err}
 if err:=s.initSyncCursors();err!=nil{_ =db.Close();return nil,err}
 return s,nil
}
func (s *Store) Close() error {return s.db.Close()}
func validateRecord(r Record) error {
 if !identifier.MatchString(r.Domain)||!identifier.MatchString(r.Metric) {return errors.New("domain and metric must be lowercase identifiers")}
 if !isFinite(r.Value) {return errors.New("value must be finite")}
 if r.Unit=="" || len(r.Unit)>64 || r.Source=="" || len(r.Source)>128 {return errors.New("unit and source are required and size-limited")}
 if r.At.IsZero() {return errors.New("observation timestamp is required")}
 if r.Method!="measured" && r.Method!="estimated" && r.Method!="reported" {return errors.New("method must be measured, estimated or reported")}
 if r.Confidence!=nil && (!isFinite(*r.Confidence)||*r.Confidence<0||*r.Confidence>1) {return errors.New("confidence must be between 0 and 1")}
 if len(r.Metadata)>262144 {return errors.New("metadata exceeds 256 KiB")}
 if len(r.Metadata)>0 {
   var x map[string]json.RawMessage
   if json.Unmarshal(r.Metadata,&x)!=nil || x==nil {return errors.New("metadata must be a JSON object")}
 }
 if len(r.SourceID)>256 {return errors.New("source_id is too long")}
 return nil
}
func isFinite(v float64) bool {return !math.IsNaN(v)&&!math.IsInf(v,0)}
func (s *Store) Add(ctx context.Context, r Record) (Record,error) {
 if err:=validateRecord(r);err!=nil{return Record{},err}
 if r.ID=="" {id,err:=newID();if err!=nil{return Record{},err};r.ID=id}
 if len(r.Metadata)==0 {r.Metadata=json.RawMessage(`{}`)}
 r.At=r.At.UTC();r.CreatedAt=time.Now().UTC()
 var confidence any
 if r.Confidence!=nil {confidence=*r.Confidence}
 var sourceID any
 if r.SourceID!="" {sourceID=r.SourceID}
 var replaces any
 if r.Replaces!="" {replaces=r.Replaces}
 tx,err:=s.db.BeginTx(ctx,nil)
 if err!=nil{return Record{},err}
 defer tx.Rollback()
 if r.Replaces!="" {
   var domain,metric string
   err=tx.QueryRowContext(ctx,"SELECT domain,metric FROM records WHERE id=? AND id NOT IN (SELECT replaces FROM records WHERE replaces IS NOT NULL)",r.Replaces).Scan(&domain,&metric)
   if errors.Is(err,sql.ErrNoRows) {return Record{},errors.New("replaced record does not exist or is no longer current")}
   if err!=nil{return Record{},err}
   if domain!=r.Domain || metric!=r.Metric {return Record{},errors.New("correction must retain domain and metric")}
 }
 _,err=tx.ExecContext(ctx,`INSERT INTO records
 (id,domain,metric,value,unit,at,source,source_id,method,confidence,metadata,replaces,created_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
 r.ID,r.Domain,r.Metric,r.Value,r.Unit,r.At.Format(time.RFC3339Nano),r.Source,sourceID,r.Method,confidence,string(r.Metadata),replaces,r.CreatedAt.Format(time.RFC3339Nano))
 if err!=nil {
   if strings.Contains(strings.ToLower(err.Error()),"unique") {return Record{},ErrConflict}
   return Record{},err
 }
 if err=tx.Commit();err!=nil{return Record{},err}
 return r,nil
}
func (s *Store) List(ctx context.Context, domain,metric string, limit int, includeHistory bool) ([]Record,error) {
 if domain!=""&&!identifier.MatchString(domain) {return nil,errors.New("invalid domain")}
 if metric!=""&&!identifier.MatchString(metric) {return nil,errors.New("invalid metric")}
 if limit<1 || limit>500 {return nil,errors.New("limit must be 1 to 500")}
 q:=`SELECT id,domain,metric,value,unit,at,source,source_id,method,confidence,metadata,replaces,created_at FROM records WHERE 1=1`
 args:=[]any{}
 if domain!="" {q+=" AND domain=?";args=append(args,domain)}
 if metric!="" {q+=" AND metric=?";args=append(args,metric)}
 if !includeHistory {q+=" AND id NOT IN (SELECT replaces FROM records WHERE replaces IS NOT NULL)"}
 q+=" ORDER BY at DESC,created_at DESC LIMIT ?";args=append(args,limit)
 rows,err:=s.db.QueryContext(ctx,q,args...)
 if err!=nil{return nil,err}
 defer rows.Close()
 records:=[]Record{}
 for rows.Next() {
  var r Record
  var at,created string
  var sourceID,replaces sql.NullString
  var confidence sql.NullFloat64
  var raw string
  if err=rows.Scan(&r.ID,&r.Domain,&r.Metric,&r.Value,&r.Unit,&at,&r.Source,&sourceID,&r.Method,&confidence,&raw,&replaces,&created);err!=nil{return nil,err}
  r.At,err=time.Parse(time.RFC3339Nano,at);if err!=nil{return nil,err}
  r.CreatedAt,err=time.Parse(time.RFC3339Nano,created);if err!=nil{return nil,err}
  if confidence.Valid {v:=confidence.Float64;r.Confidence=&v}
  r.SourceID=sourceID.String;r.Replaces=replaces.String;r.Metadata=json.RawMessage(raw)
  records=append(records,r)
 }
 return records,rows.Err()
}
func validatePlan(p Plan) error {
 if p.Kind!="workout"&&p.Kind!="meal" {return errors.New("plan kind must be workout or meal")}
 if strings.TrimSpace(p.Title)=="" || len(p.Title)>200 {return errors.New("plan title is required and at most 200 characters")}
 seen:=map[string]bool{}
 if len(p.Sessions)>2000 {return errors.New("too many sessions")}
 for _,s:=range p.Sessions {
   if s.ID=="" || seen[s.ID] || len(s.ID)>128 {return errors.New("session IDs must be unique and nonempty")}
   seen[s.ID]=true
   if _,err:=time.Parse("2006-01-02",s.Date);err!=nil{return fmt.Errorf("session %s date: %w",s.ID,err)}
   if strings.TrimSpace(s.Title)=="" {return errors.New("session title is required")}
   for _,e:=range s.Exercises {if e.Name==""||e.Sets<0||e.Repetitions<0{return errors.New("invalid exercise")}}
   for _,item:=range s.Items{if err:=validateMealItem(item);err!=nil{return err}}
 }
 return nil
}
func (s *Store) SavePlan(ctx context.Context,p Plan,expected int) (Plan,error) {
 if err:=validatePlan(p);err!=nil{return Plan{},err}
 if expected<0 {return Plan{},errors.New("expected revision cannot be negative")}
 if p.ID=="" {
  if expected!=0 {return Plan{},errors.New("new plans require expected_revision=0")}
  id,err:=newID();if err!=nil{return Plan{},err};p.ID=id
 }
 p.UpdatedAt=time.Now().UTC()
 sessions,err:=json.Marshal(p.Sessions);if err!=nil{return Plan{},err}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Plan{},err}
 defer tx.Rollback()
 if expected==0 {
  p.Revision=1
  _,err=tx.ExecContext(ctx,"INSERT INTO plans(id,kind,title,sessions,revision,updated_at) VALUES(?,?,?,?,?,?)",p.ID,p.Kind,p.Title,string(sessions),p.Revision,p.UpdatedAt.Format(time.RFC3339Nano))
 }else{
  p.Revision=expected+1
  result,e:=tx.ExecContext(ctx,"UPDATE plans SET title=?,sessions=?,revision=?,updated_at=? WHERE id=? AND revision=? AND kind=?",p.Title,string(sessions),p.Revision,p.UpdatedAt.Format(time.RFC3339Nano),p.ID,expected,p.Kind)
  if e!=nil{return Plan{},e}
  n,e:=result.RowsAffected();if e!=nil{return Plan{},e}
  if n==0{return Plan{},ErrConflict}
 }
 if err!=nil {
  if strings.Contains(strings.ToLower(err.Error()),"unique") {return Plan{},ErrConflict}
  return Plan{},err
 }
 snapshot,_:=json.Marshal(p)
 _,err=tx.ExecContext(ctx,"INSERT INTO plan_history(plan_id,revision,document,changed_at) VALUES(?,?,?,?)",p.ID,p.Revision,string(snapshot),p.UpdatedAt.Format(time.RFC3339Nano))
 if err!=nil{return Plan{},err}
 if err=tx.Commit();err!=nil{return Plan{},err}
 return p,nil
}
func (s *Store) GetPlan(ctx context.Context,id string) (Plan,error) {
 var p Plan
 var raw,updated string
 err:=s.db.QueryRowContext(ctx,"SELECT id,kind,title,sessions,revision,updated_at FROM plans WHERE id=?",id).Scan(&p.ID,&p.Kind,&p.Title,&raw,&p.Revision,&updated)
 if err!=nil{return Plan{},err}
 if err=json.Unmarshal([]byte(raw),&p.Sessions);err!=nil{return Plan{},err}
 p.UpdatedAt,err=time.Parse(time.RFC3339Nano,updated)
 return p,err
}
func (s *Store) Plans(ctx context.Context) ([]Plan,error) {
 rows,err:=s.db.QueryContext(ctx,"SELECT id FROM plans ORDER BY updated_at DESC LIMIT 100")
 if err!=nil{return nil,err}
 ids:=[]string{}
 for rows.Next(){var id string;if e:=rows.Scan(&id);e!=nil {rows.Close();return nil,e};ids=append(ids,id)}
 err=rows.Err();_ =rows.Close();if err!=nil{return nil,err}
 out:=[]Plan{}
 for _,id:=range ids {p,e:=s.GetPlan(ctx,id);if e!=nil{return nil,e};out=append(out,p)}
 return out,nil
}
func (s *Store) Pending(ctx context.Context,planID string,asOf time.Time) ([]PendingSession,error) {
 p,err:=s.GetPlan(ctx,planID);if err!=nil{return nil,err}
 if p.Kind!="workout" {return nil,errors.New("pending sessions require a workout plan")}
 out:=[]PendingSession{}
 for _,session:=range p.Sessions {
  day,e:=time.Parse("2006-01-02",session.Date);if e!=nil{return nil,e}
  if day.After(asOf) {continue}
  var decision,decidedFor string
  e=s.db.QueryRowContext(ctx,"SELECT state,scheduled_date FROM session_decisions WHERE plan_id=? AND session_id=?",planID,session.ID).Scan(&decision,&decidedFor)
  if e!=nil && !errors.Is(e,sql.ErrNoRows){return nil,e}
  if e==nil && decidedFor==session.Date {continue}
  var count int
  e=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM records WHERE domain='workout' AND metric='session'
    AND json_extract(metadata,'$.plan_id')=? AND json_extract(metadata,'$.session_id')=?
    AND id NOT IN (SELECT replaces FROM records WHERE replaces IS NOT NULL)`,planID,session.ID).Scan(&count)
  if e!=nil{return nil,e}
  if count!=0{continue}
  var partial int
  e=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM training_sets WHERE plan_id=? AND session_id=?
    AND id NOT IN(SELECT replaces FROM training_sets WHERE replaces IS NOT NULL)`,planID,session.ID).Scan(&partial)
  if e!=nil{return nil,e}
  state:="unconfirmed"
  if partial>0{state="partially_logged"}
  out=append(out,PendingSession{PlanID:planID,Session:session,State:state})
 }
 return out,nil
}
func (s *Store) DecideSession(ctx context.Context,planID,sessionID,state string) error {
 if state!="completed"&&state!="missed"&&state!="skipped" {return errors.New("invalid session state")}
 p,err:=s.GetPlan(ctx,planID);if err!=nil{return err}
 scheduledDate:=""
 for _,session:=range p.Sessions{if session.ID==sessionID{scheduledDate=session.Date;break}}
 if scheduledDate==""{return errors.New("session not found")}
 if state=="missed"{
  var observations,sets int
  err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM records WHERE domain='workout' AND metric='session'
   AND json_extract(metadata,'$.plan_id')=? AND json_extract(metadata,'$.session_id')=?
   AND id NOT IN (SELECT replaces FROM records WHERE replaces IS NOT NULL)`,planID,sessionID).Scan(&observations)
  if err!=nil{return err}
  err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM training_sets WHERE plan_id=? AND session_id=?
   AND id NOT IN (SELECT replaces FROM training_sets WHERE replaces IS NOT NULL)`,planID,sessionID).Scan(&sets)
  if err!=nil{return err}
  if observations>0||sets>0{return errors.New("workout has logged activity; reconcile the existing record before marking it missed")}
 }
 _,err=s.db.ExecContext(ctx,`INSERT INTO session_decisions(plan_id,session_id,state,scheduled_date,decided_at)
 VALUES(?,?,?,?,?) ON CONFLICT(plan_id,session_id) DO UPDATE SET state=excluded.state,scheduled_date=excluded.scheduled_date,decided_at=excluded.decided_at`,
 planID,sessionID,state,scheduledDate,time.Now().UTC().Format(time.RFC3339Nano))
 return err
}
func (s *Store) SetGoal(ctx context.Context,metric string,value float64,unit string) error {
 if !identifier.MatchString(metric)||!isFinite(value)||unit=="" {return errors.New("invalid goal")}
 _,err:=s.db.ExecContext(ctx,`INSERT INTO goals(metric,value,unit,updated_at) VALUES(?,?,?,?)
 ON CONFLICT(metric) DO UPDATE SET value=excluded.value,unit=excluded.unit,updated_at=excluded.updated_at`,
 metric,value,unit,time.Now().UTC().Format(time.RFC3339Nano))
 return err
}
func (s *Store) Goals(ctx context.Context) (map[string]any,error) {
 rows,err:=s.db.QueryContext(ctx,"SELECT metric,value,unit FROM goals ORDER BY metric")
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=map[string]any{}
 for rows.Next(){var metric,unit string;var value float64;if e:=rows.Scan(&metric,&value,&unit);e!=nil{return nil,e};out[metric]=map[string]any{"value":value,"unit":unit}}
 return out,rows.Err()
}

func NewID() (string,error) {return newID()}

// AddMany commits related observations together or writes none of them.
func (s *Store) AddMany(ctx context.Context,input []Record) ([]Record,error) {
 if len(input)==0 || len(input)>150{return nil,errors.New("invalid observation batch size")}
 prepared:=make([]Record,0,len(input))
 now:=time.Now().UTC()
 for _,r:=range input {
  if err:=validateRecord(r);err!=nil{return nil,err}
  if r.ID=="" {id,err:=newID();if err!=nil{return nil,err};r.ID=id}
  if len(r.Metadata)==0{r.Metadata=json.RawMessage(`{}`)}
  r.At=r.At.UTC();r.CreatedAt=now
  if r.Replaces!="" || r.SourceID!="" {return nil,errors.New("batch observations do not support corrections or external IDs")}
  prepared=append(prepared,r)
 }
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return nil,err}
 defer tx.Rollback()
 for _,r:=range prepared {
  var confidence any
  if r.Confidence!=nil{confidence=*r.Confidence}
  _,err=tx.ExecContext(ctx,`INSERT INTO records
  (id,domain,metric,value,unit,at,source,source_id,method,confidence,metadata,replaces,created_at)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,r.ID,r.Domain,r.Metric,r.Value,r.Unit,r.At.Format(time.RFC3339Nano),r.Source,nil,r.Method,confidence,string(r.Metadata),nil,r.CreatedAt.Format(time.RFC3339Nano))
  if err!=nil{return nil,err}
 }
 if err=tx.Commit();err!=nil{return nil,err}
 return prepared,nil
}

// RescheduleConfirmedSession moves a workout only after a user has marked it missed.
// An unlogged session is not evidence of a missed session.
func (s *Store) RescheduleConfirmedSession(ctx context.Context,planID,sessionID,targetDate string,expected int)(Plan,error){
 target,err:=time.Parse("2006-01-02",targetDate)
 if err!=nil || target.Format("2006-01-02")!=targetDate{return Plan{},errors.New("target_date must be a valid YYYY-MM-DD date")}
 p,err:=s.GetPlan(ctx,planID)
 if err!=nil{return Plan{},err}
 if p.Kind!="workout"{return Plan{},errors.New("rescheduling applies only to workout plans")}
 if p.Revision!=expected{return Plan{},ErrConflict}
 var state,decidedFor string
 err=s.db.QueryRowContext(ctx,"SELECT state,scheduled_date FROM session_decisions WHERE plan_id=? AND session_id=?",planID,sessionID).Scan(&state,&decidedFor)
 if errors.Is(err,sql.ErrNoRows){return Plan{},errors.New("session outcome is unconfirmed; ask whether the workout was completed before rescheduling")}
 if err!=nil{return Plan{},err}
 if state!="missed" {return Plan{},errors.New("session must be explicitly confirmed missed before rescheduling")}
 var observations,sets int
 err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM records WHERE domain='workout' AND metric='session'
  AND json_extract(metadata,'$.plan_id')=? AND json_extract(metadata,'$.session_id')=?
  AND id NOT IN (SELECT replaces FROM records WHERE replaces IS NOT NULL)`,planID,sessionID).Scan(&observations)
 if err!=nil{return Plan{},err}
 err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM training_sets WHERE plan_id=? AND session_id=?
  AND id NOT IN (SELECT replaces FROM training_sets WHERE replaces IS NOT NULL)`,planID,sessionID).Scan(&sets)
 if err!=nil{return Plan{},err}
 if observations>0||sets>0{return Plan{},errors.New("recorded workout activity must be reconciled before moving a missed session")}
 found:=false
 for i,session:=range p.Sessions{
  if session.ID!=sessionID {
   if session.Date==targetDate{return Plan{},errors.New("target date already contains a session; choose another date or edit the full plan")}
   continue
  }
  if decidedFor!=session.Date{return Plan{},errors.New("missed status belongs to an earlier schedule date; confirm the current session outcome")}
  previous,err:=time.Parse("2006-01-02",session.Date)
  if err!=nil{return Plan{},err}
  if !target.After(previous){return Plan{},errors.New("target date must follow the missed session")}
  p.Sessions[i].Date=targetDate
  found=true
 }
 if !found{return Plan{},errors.New("session not found")}
 return s.SavePlan(ctx,p,expected)
}
