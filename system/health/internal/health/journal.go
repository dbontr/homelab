package health

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"
)

var journalCategories=map[string]bool{
 "symptom":true,"allergy":true,"medication":true,"supplement":true,"laboratory":true,
 "injury":true,"rehabilitation":true,"mobility":true,"digestive":true,
 "oral":true,"reproductive":true,"mental_wellbeing":true,"substance":true,
 "hydration":true,"nutrition_note":true,"training_note":true,"sleep_note":true,
 "environment":true,"experiment":true,"recommendation":true,"technique_review":true,
}

type JournalEntry struct {
 ID string `json:"id"`
 Category string `json:"category"`
 Title string `json:"title"`
 OccurredAt time.Time `json:"occurred_at"`
 Notes string `json:"notes,omitempty"`
 Attributes json.RawMessage `json:"attributes"`
 Source string `json:"source"`
 Revision int `json:"revision"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
}

func(s *Store) initJournal() error {
 for _,statement:=range []string{
  `CREATE TABLE IF NOT EXISTS journal_entries(
   id TEXT PRIMARY KEY, category TEXT NOT NULL, title TEXT NOT NULL,
   occurred_at TEXT NOT NULL, notes TEXT NOT NULL, attributes TEXT NOT NULL,
   source TEXT NOT NULL, revision INTEGER NOT NULL,
   created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
  "CREATE INDEX IF NOT EXISTS journal_category_date ON journal_entries(category,occurred_at DESC)",
  `CREATE TABLE IF NOT EXISTS journal_revisions(
   entry_id TEXT NOT NULL REFERENCES journal_entries(id),
   revision INTEGER NOT NULL, document TEXT NOT NULL, changed_at TEXT NOT NULL,
   PRIMARY KEY(entry_id,revision))`,
 }{
  if _,err:=s.db.Exec(statement);err!=nil{return fmt.Errorf("journal setup: %w",err)}
 }
 return nil
}
func validateJournal(e JournalEntry) error {
 if !journalCategories[e.Category]{return errors.New("unknown health journal category")}
 if strings.TrimSpace(e.Title)==""||len(e.Title)>250{return errors.New("journal title must contain 1 to 250 characters")}
 if e.OccurredAt.IsZero(){return errors.New("occurred_at is required")}
 if e.Source==""||len(e.Source)>128{return errors.New("source is required and limited to 128 characters")}
 if len(e.Notes)>32<<10{return errors.New("notes exceed 32 KiB")}
 if len(e.Attributes)>64<<10{return errors.New("attributes exceed 64 KiB")}
 if len(e.Attributes)>0 {
  var object map[string]json.RawMessage
  if json.Unmarshal(e.Attributes,&object)!=nil||object==nil{return errors.New("attributes must be a JSON object")}
 }
 return nil
}
func(s *Store) SaveJournal(ctx context.Context,e JournalEntry,expected int)(JournalEntry,error){
 if expected<0{return JournalEntry{},errors.New("expected_revision must be nonnegative")}
 if err:=validateJournal(e);err!=nil{return JournalEntry{},err}
 if len(e.Attributes)==0{e.Attributes=json.RawMessage(`{}`)}
 now:=time.Now().UTC();e.OccurredAt=e.OccurredAt.UTC();e.UpdatedAt=now
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return JournalEntry{},err}
 defer tx.Rollback()
 if expected==0{
  if e.ID!=""{return JournalEntry{},errors.New("new journal records must omit id")}
  e.ID,err=newID();if err!=nil{return JournalEntry{},err}
  e.Revision=1;e.CreatedAt=now
  _,err=tx.ExecContext(ctx,`INSERT INTO journal_entries
    (id,category,title,occurred_at,notes,attributes,source,revision,created_at,updated_at)
    VALUES (?,?,?,?,?,?,?,?,?,?)`,
    e.ID,e.Category,e.Title,e.OccurredAt.Format(time.RFC3339Nano),
    e.Notes,string(e.Attributes),e.Source,e.Revision,
    now.Format(time.RFC3339Nano),now.Format(time.RFC3339Nano))
 }else{
  if e.ID==""{return JournalEntry{},errors.New("id required for edit")}
  var previousCategory,created string
  err=tx.QueryRowContext(ctx,`SELECT category,created_at FROM journal_entries
   WHERE id=? AND revision=?`,e.ID,expected).Scan(&previousCategory,&created)
  if errors.Is(err,sql.ErrNoRows){return JournalEntry{},ErrConflict}
  if err!=nil{return JournalEntry{},err}
  if previousCategory!=e.Category{return JournalEntry{},errors.New("journal category cannot change")}
  e.CreatedAt,err=time.Parse(time.RFC3339Nano,created);if err!=nil{return JournalEntry{},err}
  e.Revision=expected+1
  var result sql.Result
  result,err=tx.ExecContext(ctx,`UPDATE journal_entries SET
    title=?,occurred_at=?,notes=?,attributes=?,source=?,revision=?,updated_at=?
    WHERE id=? AND revision=?`,
    e.Title,e.OccurredAt.Format(time.RFC3339Nano),e.Notes,
    string(e.Attributes),e.Source,e.Revision,now.Format(time.RFC3339Nano),e.ID,expected)
  if err==nil{var n int64;n,err=result.RowsAffected();if err==nil&&n==0{return JournalEntry{},ErrConflict}}
 }
 if err!=nil{return JournalEntry{},err}
 snapshot,err:=json.Marshal(e);if err!=nil{return JournalEntry{},err}
 _,err=tx.ExecContext(ctx,`INSERT INTO journal_revisions(entry_id,revision,document,changed_at)
 VALUES(?,?,?,?)`,e.ID,e.Revision,string(snapshot),now.Format(time.RFC3339Nano))
 if err!=nil{return JournalEntry{},err}
 if err=tx.Commit();err!=nil{return JournalEntry{},err}
 return e,nil
}
func(s *Store) GetJournal(ctx context.Context,id string)(JournalEntry,error){
 var e JournalEntry;var occurred,created,updated,attributes string
 err:=s.db.QueryRowContext(ctx,`SELECT id,category,title,occurred_at,notes,attributes,
 source,revision,created_at,updated_at FROM journal_entries WHERE id=?`,id).
 Scan(&e.ID,&e.Category,&e.Title,&occurred,&e.Notes,&attributes,&e.Source,&e.Revision,&created,&updated)
 if err!=nil{return JournalEntry{},err}
 e.Attributes=json.RawMessage(attributes)
 e.OccurredAt,err=time.Parse(time.RFC3339Nano,occurred);if err!=nil{return JournalEntry{},err}
 e.CreatedAt,err=time.Parse(time.RFC3339Nano,created);if err!=nil{return JournalEntry{},err}
 e.UpdatedAt,err=time.Parse(time.RFC3339Nano,updated)
 return e,err
}
func(s *Store) ListJournal(ctx context.Context,category string,limit int)([]JournalEntry,error){
 if category!=""&&!journalCategories[category]{return nil,errors.New("unknown category")}
 if limit<1||limit>200{return nil,errors.New("limit must be 1 to 200")}
 q:="SELECT id FROM journal_entries";args:=[]any{}
 if category!=""{q+=" WHERE category=?";args=append(args,category)}
 q+=" ORDER BY occurred_at DESC,id DESC LIMIT ?";args=append(args,limit)
 rows,err:=s.db.QueryContext(ctx,q,args...);if err!=nil{return nil,err}
 ids:=[]string{}
 for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{break};ids=append(ids,id)}
 if err==nil{err=rows.Err()};_ =rows.Close();if err!=nil{return nil,err}
 entries:=make([]JournalEntry,0,len(ids))
 for _,id:=range ids{e,err:=s.GetJournal(ctx,id);if err!=nil{return nil,err};entries=append(entries,e)}
 return entries,nil
}
func(s *Store) JournalHistory(ctx context.Context,id string)([]JournalEntry,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT document FROM journal_revisions WHERE entry_id=? ORDER BY revision`,id)
 if err!=nil{return nil,err}
 defer rows.Close()
 entries:=[]JournalEntry{}
 for rows.Next(){
  var doc string
  if err=rows.Scan(&doc);err!=nil{return nil,err}
  var e JournalEntry
  if err=json.Unmarshal([]byte(doc),&e);err!=nil{return nil,err}
  entries=append(entries,e)
 }
 return entries,rows.Err()
}
