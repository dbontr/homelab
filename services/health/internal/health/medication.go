package health

import(
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"
)

type MedicationRegimen struct{
 ID string `json:"id"`
 Medication string `json:"medication"`
 Dose float64 `json:"dose"`
 Unit string `json:"unit"`
 Route string `json:"route"`
 Times []string `json:"times"`
 Timezone string `json:"timezone"`
 StartDate string `json:"start_date"`
 EndDate string `json:"end_date,omitempty"`
 Status string `json:"status"`
 Instructions string `json:"instructions,omitempty"`
 Revision int `json:"revision"`
 UpdatedAt time.Time `json:"updated_at"`
}
type MedicationDose struct{
 ID string `json:"id"`
 RegimenID string `json:"regimen_id"`
 ScheduledFor time.Time `json:"scheduled_for"`
 TakenAt *time.Time `json:"taken_at,omitempty"`
 Outcome string `json:"outcome"`
 Source string `json:"source"`
 Notes string `json:"notes,omitempty"`
 CreatedAt time.Time `json:"created_at"`
}

func(s *Store)initMedications()error{
 for _,query:=range []string{
  `CREATE TABLE IF NOT EXISTS medication_regimens(
   id TEXT PRIMARY KEY,document TEXT NOT NULL,revision INTEGER NOT NULL,
   updated_at TEXT NOT NULL)`,
  `CREATE TABLE IF NOT EXISTS medication_regimen_history(
   regimen_id TEXT NOT NULL REFERENCES medication_regimens(id),
   revision INTEGER NOT NULL,document TEXT NOT NULL,changed_at TEXT NOT NULL,
   PRIMARY KEY(regimen_id,revision))`,
  `CREATE TABLE IF NOT EXISTS medication_doses(
   id TEXT PRIMARY KEY,regimen_id TEXT NOT NULL REFERENCES medication_regimens(id),
   scheduled_for TEXT NOT NULL,taken_at TEXT,outcome TEXT NOT NULL,
   source TEXT NOT NULL,notes TEXT NOT NULL,created_at TEXT NOT NULL,
   UNIQUE(regimen_id,scheduled_for))`,
 }{
  if _,err:=s.db.Exec(query);err!=nil{return fmt.Errorf("medication schema: %w",err)}
 }
 return nil
}
func validateMedication(m MedicationRegimen)error{
 if strings.TrimSpace(m.Medication)==""||len(m.Medication)>200{return errors.New("medication name is required")}
 if !isFinite(m.Dose)||m.Dose<=0||m.Dose>1000000{return errors.New("dose must be a positive finite number")}
 if m.Unit==""||len(m.Unit)>48||m.Route==""||len(m.Route)>80{return errors.New("unit and route are required")}
 if _,err:=time.LoadLocation(m.Timezone);err!=nil{return errors.New("timezone must be an IANA timezone")}
 begin,err:=time.Parse("2006-01-02",m.StartDate);if err!=nil{return errors.New("start_date must be YYYY-MM-DD")}
 if m.EndDate!=""{
  end,err:=time.Parse("2006-01-02",m.EndDate)
  if err!=nil||end.Before(begin){return errors.New("end_date must be a date after start_date")}
 }
 if m.Status!="active"&&m.Status!="paused"&&m.Status!="completed"{return errors.New("invalid regimen status")}
 if len(m.Times)>24||len(m.Times)==0{return errors.New("1 to 24 scheduled local clock times are required")}
 seen:=map[string]bool{}
 for _,value:=range m.Times{
  t,err:=time.Parse("15:04",value)
  if err!=nil||t.Format("15:04")!=value||seen[value]{return errors.New("times must be unique 24-hour HH:MM values")}
  seen[value]=true
 }
 if len(m.Instructions)>4096{return errors.New("instructions exceed size limit")}
 return nil
}
func(s *Store)SaveMedication(ctx context.Context,m MedicationRegimen,expected int)(MedicationRegimen,error){
 if expected<0{return MedicationRegimen{},errors.New("expected_revision must be nonnegative")}
 if err:=validateMedication(m);err!=nil{return MedicationRegimen{},err}
 m.UpdatedAt=time.Now().UTC()
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return MedicationRegimen{},err}
 defer tx.Rollback()
 if expected==0{
  if m.ID!=""{return MedicationRegimen{},errors.New("new medication regimens must omit id")}
  m.ID,err=newID();if err!=nil{return MedicationRegimen{},err}
  m.Revision=1
 }else{
  if m.ID==""{return MedicationRegimen{},errors.New("regimen id is required")}
  m.Revision=expected+1
 }
 data,err:=json.Marshal(m);if err!=nil{return MedicationRegimen{},err}
 stamp:=m.UpdatedAt.Format(time.RFC3339Nano)
 if expected==0{
  _,err=tx.ExecContext(ctx,`INSERT INTO medication_regimens(id,document,revision,updated_at)
   VALUES(?,?,?,?)`,m.ID,string(data),m.Revision,stamp)
 }else{
  var result sql.Result
  result,err=tx.ExecContext(ctx,`UPDATE medication_regimens SET document=?,revision=?,updated_at=?
   WHERE id=? AND revision=?`,string(data),m.Revision,stamp,m.ID,expected)
  if err==nil{
   var n int64;n,err=result.RowsAffected()
   if err==nil&&n==0{return MedicationRegimen{},ErrConflict}
  }
 }
 if err!=nil{return MedicationRegimen{},err}
 _,err=tx.ExecContext(ctx,`INSERT INTO medication_regimen_history(regimen_id,revision,document,changed_at)
  VALUES(?,?,?,?)`,m.ID,m.Revision,string(data),stamp)
 if err!=nil{return MedicationRegimen{},err}
 if err=tx.Commit();err!=nil{return MedicationRegimen{},err}
 return m,nil
}
func(s *Store)GetMedication(ctx context.Context,id string)(MedicationRegimen,error){
 var payload string
 if err:=s.db.QueryRowContext(ctx,"SELECT document FROM medication_regimens WHERE id=?",id).Scan(&payload);err!=nil{
  return MedicationRegimen{},err
 }
 var m MedicationRegimen
 if err:=json.Unmarshal([]byte(payload),&m);err!=nil{return MedicationRegimen{},err}
 return m,nil
}
func(s *Store)ListMedications(ctx context.Context,includeInactive bool)([]MedicationRegimen,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT document FROM medication_regimens ORDER BY updated_at DESC LIMIT 500`)
 if err!=nil{return nil,err}
 defer rows.Close()
 records:=[]MedicationRegimen{}
 for rows.Next(){
  var raw string
  if err=rows.Scan(&raw);err!=nil{return nil,err}
  var m MedicationRegimen
  if err=json.Unmarshal([]byte(raw),&m);err!=nil{return nil,err}
  if !includeInactive&&m.Status!="active"{continue}
  records=append(records,m)
 }
 return records,rows.Err()
}
func(s *Store)MedicationHistory(ctx context.Context,id string)([]MedicationRegimen,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT document FROM medication_regimen_history
 WHERE regimen_id=? ORDER BY revision ASC`,id)
 if err!=nil{return nil,err}
 defer rows.Close()
 values:=[]MedicationRegimen{}
 for rows.Next(){
  var raw string
  if err=rows.Scan(&raw);err!=nil{return nil,err}
  var m MedicationRegimen
  if err=json.Unmarshal([]byte(raw),&m);err!=nil{return nil,err}
  values=append(values,m)
 }
 return values,rows.Err()
}
func(s *Store)LogMedicationDose(ctx context.Context,d MedicationDose)(MedicationDose,error){
 if d.RegimenID==""||d.ScheduledFor.IsZero()||d.Source==""||len(d.Source)>128||
  len(d.Notes)>4096{return MedicationDose{},errors.New("regimen, scheduled time and source are required")}
 if d.Outcome!="taken"&&d.Outcome!="skipped"&&d.Outcome!="uncertain"{
  return MedicationDose{},errors.New("outcome must be taken, skipped or uncertain")
 }
 if d.Outcome=="taken"&&d.TakenAt==nil{return MedicationDose{},errors.New("taken outcome requires actual taken_at")}
 if d.Outcome!="taken"&&d.TakenAt!=nil{return MedicationDose{},errors.New("taken_at may only be present for taken outcome")}
 if _,err:=s.GetMedication(ctx,d.RegimenID);err!=nil{return MedicationDose{},err}
 var err error
 d.ID,err=newID();if err!=nil{return MedicationDose{},err}
 d.ScheduledFor=d.ScheduledFor.UTC()
 if d.TakenAt!=nil{converted:=d.TakenAt.UTC();d.TakenAt=&converted}
 d.CreatedAt=time.Now().UTC()
 var taken any
 if d.TakenAt!=nil{taken=d.TakenAt.Format(time.RFC3339Nano)}
 _,err=s.db.ExecContext(ctx,`INSERT INTO medication_doses
 (id,regimen_id,scheduled_for,taken_at,outcome,source,notes,created_at)
 VALUES(?,?,?,?,?,?,?,?)`,
 d.ID,d.RegimenID,d.ScheduledFor.Format(time.RFC3339Nano),taken,d.Outcome,
 d.Source,d.Notes,d.CreatedAt.Format(time.RFC3339Nano))
 if err!=nil{
  if strings.Contains(strings.ToLower(err.Error()),"unique"){return MedicationDose{},ErrConflict}
  return MedicationDose{},err
 }
 return d,nil
}
func(s *Store)ListMedicationDoses(ctx context.Context,regimenID string,limit int)([]MedicationDose,error){
 if regimenID==""||limit<1||limit>500{return nil,errors.New("invalid regimen ID or limit")}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,regimen_id,scheduled_for,taken_at,
  outcome,source,notes,created_at FROM medication_doses
  WHERE regimen_id=? ORDER BY scheduled_for DESC LIMIT ?`,regimenID,limit)
 if err!=nil{return nil,err}
 defer rows.Close()
 doses:=[]MedicationDose{}
 for rows.Next(){
  var d MedicationDose;var scheduled,created string;var taken sql.NullString
  if err=rows.Scan(&d.ID,&d.RegimenID,&scheduled,&taken,&d.Outcome,
   &d.Source,&d.Notes,&created);err!=nil{return nil,err}
  d.ScheduledFor,err=time.Parse(time.RFC3339Nano,scheduled);if err!=nil{return nil,err}
  d.CreatedAt,err=time.Parse(time.RFC3339Nano,created);if err!=nil{return nil,err}
  if taken.Valid{value,e:=time.Parse(time.RFC3339Nano,taken.String);if e!=nil{return nil,e};d.TakenAt=&value}
  doses=append(doses,d)
 }
 return doses,rows.Err()
}
