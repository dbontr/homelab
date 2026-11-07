package health

import(
 "context"
 "database/sql"
 "encoding/csv"
 "strconv"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"
)

// ExportRecords pages over the original observation stream and optionally
// retains superseded values. Clients should keep the offset small and stable.
func(s *Store)ExportRecords(ctx context.Context,domain,metric string,limit,offset int,history bool)([]Record,error){
 if limit<1||limit>500||offset<0||offset>10000000{
  return nil,errors.New("export limit must be 1 to 500 and offset 0 to 10000000")
 }
 if domain!=""&&!identifier.MatchString(domain){return nil,errors.New("invalid domain")}
 if metric!=""&&!identifier.MatchString(metric){return nil,errors.New("invalid metric")}
 q:=`SELECT id,domain,metric,value,unit,at,source,source_id,method,
 confidence,metadata,replaces,created_at FROM records WHERE 1=1`
 args:=[]any{}
 if domain!=""{q+=" AND domain=?";args=append(args,domain)}
 if metric!=""{q+=" AND metric=?";args=append(args,metric)}
 if !history{q+=" AND id NOT IN(SELECT replaces FROM records WHERE replaces IS NOT NULL)"}
 q+=" ORDER BY at DESC,id DESC LIMIT ? OFFSET ?"
 args=append(args,limit,offset)
 rows,err:=s.db.QueryContext(ctx,q,args...);if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]Record{}
 for rows.Next(){
  var r Record;var at,created,metadata string
  var externalID,replaces sql.NullString;var conf sql.NullFloat64
  if err=rows.Scan(&r.ID,&r.Domain,&r.Metric,&r.Value,&r.Unit,&at,&r.Source,
   &externalID,&r.Method,&conf,&metadata,&replaces,&created);err!=nil{return nil,err}
  r.At,err=time.Parse(time.RFC3339Nano,at);if err!=nil{return nil,err}
  r.CreatedAt,err=time.Parse(time.RFC3339Nano,created);if err!=nil{return nil,err}
  r.SourceID=externalID.String;r.Replaces=replaces.String
  if conf.Valid{v:=conf.Float64;r.Confidence=&v}
  r.Metadata=json.RawMessage(metadata);out=append(out,r)
 }
 return out,rows.Err()
}

// ImportRecords uses source identifiers to deduplicate imports atomically.
// Every record must carry a stable external identifier and valid provenance.
func(s *Store)ImportRecords(ctx context.Context,records []Record)(int,int,error){
 if len(records)<1||len(records)>200{return 0,0,errors.New("imports contain 1 to 200 records")}
 prepared:=make([]Record,0,len(records))
 now:=time.Now().UTC()
 seen:=map[string]bool{}
 for _,r:=range records{
  if r.SourceID==""||r.Replaces!=""{
   return 0,0,errors.New("imported records need a source ID and cannot supersede existing records")
  }
  if err:=validateRecord(r);err!=nil{return 0,0,err}
  key:=r.Source+"\x00"+r.SourceID
  if seen[key]{return 0,0,errors.New("import contains duplicate source IDs")}
  seen[key]=true
  id,err:=newID();if err!=nil{return 0,0,err}
  r.ID=id;r.CreatedAt=now;r.At=r.At.UTC()
  if len(r.Metadata)==0{r.Metadata=json.RawMessage(`{}`)}
  prepared=append(prepared,r)
 }
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return 0,0,err}
 defer tx.Rollback()
 imported,existing:=0,0
 for _,r:=range prepared{
  var confidence any
  if r.Confidence!=nil{confidence=*r.Confidence}
  result,err:=tx.ExecContext(ctx,`INSERT INTO records(
   id,domain,metric,value,unit,at,source,source_id,method,confidence,metadata,replaces,created_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
   ON CONFLICT(source,source_id) DO NOTHING`,
   r.ID,r.Domain,r.Metric,r.Value,r.Unit,r.At.Format(time.RFC3339Nano),r.Source,r.SourceID,
   r.Method,confidence,string(r.Metadata),nil,r.CreatedAt.Format(time.RFC3339Nano))
  if err!=nil{return 0,0,fmt.Errorf("import record: %w",err)}
  n,err:=result.RowsAffected();if err!=nil{return 0,0,err}
  if n==0{existing++}else{imported++}
 }
 if err=tx.Commit();err!=nil{return 0,0,err}
 return imported,existing,nil
}
func SimpleExportCSV(records []Record)(string,error){
 var b strings.Builder
 writer:=csv.NewWriter(&b)
 header:=[]string{"id","domain","metric","value","unit","at","source","source_id","method","confidence","replaces"}
 if err:=writer.Write(header);err!=nil{return "",err}
 for _,r:=range records{
  confidence:=""
  if r.Confidence!=nil{confidence=strconv.FormatFloat(*r.Confidence,'f',-1,64)}
  fields:=[]string{r.ID,r.Domain,r.Metric,strconv.FormatFloat(r.Value,'f',-1,64),r.Unit,
   r.At.Format(time.RFC3339Nano),r.Source,r.SourceID,r.Method,confidence,r.Replaces}
  if err:=writer.Write(fields);err!=nil{return "",err}
 }
 writer.Flush()
 return b.String(),writer.Error()
}
