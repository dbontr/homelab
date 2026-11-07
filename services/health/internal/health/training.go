package health

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "strings"
 "time"
)

type TrainingSet struct {
 ID string `json:"id"`
 PlanID string `json:"plan_id,omitempty"`
 SessionID string `json:"session_id,omitempty"`
 Exercise string `json:"exercise"`
 Activity string `json:"activity"`
 SetNumber int `json:"set_number"`
 Repetitions int `json:"repetitions,omitempty"`
 LoadKG *float64 `json:"load_kg,omitempty"`
 DurationSeconds *float64 `json:"duration_seconds,omitempty"`
 DistanceMeters *float64 `json:"distance_meters,omitempty"`
 RPE *float64 `json:"rpe,omitempty"`
 At time.Time `json:"at"`
 Source string `json:"source"`
 ClientKey string `json:"client_key,omitempty"`
 Notes string `json:"notes,omitempty"`
 Replaces string `json:"replaces,omitempty"`
 CreatedAt time.Time `json:"created_at"`
}

func(s *Store)initTraining()error{
 for _,query:=range []string{
  `CREATE TABLE IF NOT EXISTS training_sets(
    id TEXT PRIMARY KEY,plan_id TEXT NOT NULL,session_id TEXT NOT NULL,
    exercise TEXT NOT NULL,activity TEXT NOT NULL,set_number INTEGER NOT NULL,
    repetitions INTEGER NOT NULL,load_kg REAL,duration_seconds REAL,
    distance_meters REAL,rpe REAL,at TEXT NOT NULL,source TEXT NOT NULL,
    client_key TEXT,notes TEXT NOT NULL,replaces TEXT UNIQUE REFERENCES training_sets(id),
    created_at TEXT NOT NULL,UNIQUE(source,client_key))`,
  "CREATE INDEX IF NOT EXISTS training_lookup ON training_sets(exercise,at DESC)",
  "CREATE INDEX IF NOT EXISTS training_session ON training_sets(plan_id,session_id,at)",
 }{
  if _,err:=s.db.Exec(query);err!=nil{return fmt.Errorf("training database: %w",err)}
 }
 return nil
}
func checkTrainingSet(set TrainingSet)error{
 if len(strings.TrimSpace(set.Exercise))==0||len(set.Exercise)>200{
  return errors.New("exercise must contain 1 to 200 characters")
 }
 if set.Activity!="strength"&&set.Activity!="cardio"&&set.Activity!="mobility"{
  return errors.New("activity must be strength, cardio, or mobility")
 }
 if set.SetNumber<0||set.SetNumber>1000||set.Repetitions<0||set.Repetitions>100000 {
  return errors.New("set number or repetition count is outside supported range")
 }
 if set.At.IsZero(){return errors.New("training timestamp is required")}
 if set.Source==""||len(set.Source)>128{return errors.New("training source is required")}
 if len(set.Notes)>4096||len(set.PlanID)>128||len(set.SessionID)>128||len(set.ClientKey)>256{
  return errors.New("training identifiers or notes exceed size limit")
 }
 for _,measurement:=range []*float64{set.LoadKG,set.DurationSeconds,set.DistanceMeters,set.RPE}{
  if measurement!=nil&&(!isFinite(*measurement)||*measurement<0){
   return errors.New("training measurements must be finite and nonnegative")
  }
 }
 if set.RPE!=nil&&*set.RPE>10{return errors.New("RPE cannot exceed 10")}
 if set.Activity=="strength"&&set.Repetitions==0&&set.DurationSeconds==nil{
  return errors.New("strength work must include repetitions or duration")
 }
 if set.Activity=="cardio"&&set.DurationSeconds==nil&&set.DistanceMeters==nil{
  return errors.New("cardio work must have distance or duration")
 }
 if set.SessionID!=""&&set.PlanID==""{return errors.New("plan_id is required when session_id is specified")}
 return nil
}
func(s *Store)AddTrainingSet(ctx context.Context,set TrainingSet)(TrainingSet,error){
 if err:=checkTrainingSet(set);err!=nil{return TrainingSet{},err}
 if set.PlanID!=""{
  p,err:=s.GetPlan(ctx,set.PlanID);if err!=nil{return TrainingSet{},err}
  if p.Kind!="workout"{return TrainingSet{},errors.New("plan is not a workout plan")}
  if set.SessionID!=""{
   exists:=false
   for _,session:=range p.Sessions{if session.ID==set.SessionID{exists=true;break}}
   if !exists{return TrainingSet{},errors.New("workout session not found")}
  }
 }
 if set.ID=="" {var err error;set.ID,err=newID();if err!=nil{return TrainingSet{},err}}
 set.At=set.At.UTC();set.CreatedAt=time.Now().UTC()
 var load,duration,distance,rpe,clientKey,replaces any
 if set.LoadKG!=nil{load=*set.LoadKG}
 if set.DurationSeconds!=nil{duration=*set.DurationSeconds}
 if set.DistanceMeters!=nil{distance=*set.DistanceMeters}
 if set.RPE!=nil{rpe=*set.RPE}
 if set.ClientKey!=""{clientKey=set.ClientKey}
 if set.Replaces!=""{replaces=set.Replaces}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return TrainingSet{},err}
 defer tx.Rollback()
 if set.Replaces!=""{
  var planID,sessionID,exercise string
  err=tx.QueryRowContext(ctx,`SELECT plan_id,session_id,exercise FROM training_sets WHERE id=?
   AND id NOT IN (SELECT replaces FROM training_sets WHERE replaces IS NOT NULL)`,set.Replaces).
   Scan(&planID,&sessionID,&exercise)
  if errors.Is(err,sql.ErrNoRows){return TrainingSet{},errors.New("superseded training entry does not exist or is not current")}
  if err!=nil{return TrainingSet{},err}
  if planID!=set.PlanID||sessionID!=set.SessionID||exercise!=set.Exercise{
   return TrainingSet{},errors.New("correction must keep plan, session and exercise identity")
  }
 }
 _,err=tx.ExecContext(ctx,`INSERT INTO training_sets(
  id,plan_id,session_id,exercise,activity,set_number,repetitions,load_kg,
  duration_seconds,distance_meters,rpe,at,source,client_key,notes,replaces,created_at)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
  set.ID,set.PlanID,set.SessionID,set.Exercise,set.Activity,set.SetNumber,
  set.Repetitions,load,duration,distance,rpe,set.At.Format(time.RFC3339Nano),
  set.Source,clientKey,set.Notes,replaces,set.CreatedAt.Format(time.RFC3339Nano))
 if err!=nil{
  if strings.Contains(strings.ToLower(err.Error()),"unique"){return TrainingSet{},ErrConflict}
  return TrainingSet{},err
 }
 if err=tx.Commit();err!=nil{return TrainingSet{},err}
 return set,nil
}
func(s *Store)ListTrainingSets(ctx context.Context,planID,exercise string,limit int,history bool)([]TrainingSet,error){
 if limit<1||limit>500{return nil,errors.New("limit must be 1 to 500")}
 q:=`SELECT id,plan_id,session_id,exercise,activity,set_number,repetitions,load_kg,
 duration_seconds,distance_meters,rpe,at,source,client_key,notes,replaces,created_at
 FROM training_sets WHERE 1=1`
 args:=[]any{}
 if planID!=""{q+=" AND plan_id=?";args=append(args,planID)}
 if exercise!=""{q+=" AND exercise=?";args=append(args,exercise)}
 if !history{q+=" AND id NOT IN (SELECT replaces FROM training_sets WHERE replaces IS NOT NULL)"}
 q+=" ORDER BY at DESC,created_at DESC LIMIT ?";args=append(args,limit)
 rows,err:=s.db.QueryContext(ctx,q,args...);if err!=nil{return nil,err}
 defer rows.Close()
 sets:=[]TrainingSet{}
 for rows.Next(){
  var r TrainingSet
  var load,duration,distance,rpe sql.NullFloat64
  var clientKey,replaces sql.NullString
  var at,created string
  err=rows.Scan(&r.ID,&r.PlanID,&r.SessionID,&r.Exercise,&r.Activity,
   &r.SetNumber,&r.Repetitions,&load,&duration,&distance,&rpe,&at,&r.Source,
   &clientKey,&r.Notes,&replaces,&created)
  if err!=nil{return nil,err}
  if load.Valid{v:=load.Float64;r.LoadKG=&v}
  if duration.Valid{v:=duration.Float64;r.DurationSeconds=&v}
  if distance.Valid{v:=distance.Float64;r.DistanceMeters=&v}
  if rpe.Valid{v:=rpe.Float64;r.RPE=&v}
  r.ClientKey=clientKey.String;r.Replaces=replaces.String
  r.At,err=time.Parse(time.RFC3339Nano,at);if err!=nil{return nil,err}
  r.CreatedAt,err=time.Parse(time.RFC3339Nano,created);if err!=nil{return nil,err}
  sets=append(sets,r)
 }
 return sets,rows.Err()
}
