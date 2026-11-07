package health

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"
 "encoding/json"
)
func testStore(t *testing.T) *Store {
 t.Helper()
 s,err:=Open(filepath.Join(t.TempDir(),"health.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ =s.Close()})
 return s
}
func TestObservationCorrectionPreservesHistory(t *testing.T) {
 ctx:=context.Background();s:=testStore(t)
 before,err:=s.Add(ctx,Record{Domain:"sleep",Metric:"duration_hours",Value:6.5,Unit:"h",At:time.Now(),Source:"manual",Method:"reported"})
 if err!=nil{t.Fatal(err)}
 after,err:=s.Add(ctx,Record{Domain:"sleep",Metric:"duration_hours",Value:7.5,Unit:"h",At:before.At,Source:"manual",Method:"reported",Replaces:before.ID})
 if err!=nil{t.Fatal(err)}
 current,err:=s.List(ctx,"sleep","duration_hours",100,false)
 if err!=nil||len(current)!=1||current[0].ID!=after.ID{t.Fatalf("current observations: %+v, %v",current,err)}
 history,err:=s.List(ctx,"sleep","duration_hours",100,true)
 if err!=nil||len(history)!=2{t.Fatalf("historical observations: %+v, %v",history,err)}
 _,err=s.Add(ctx,Record{Domain:"sleep",Metric:"duration_hours",Value:8,Unit:"h",At:before.At,Source:"manual",Method:"reported",Replaces:before.ID})
 if err==nil{t.Fatal("accepted two corrections for the same record")}
}
func TestPlansRequireMatchingRevisionAndPreserveUnconfirmed(t *testing.T) {
 ctx:=context.Background();s:=testStore(t)
 p,err:=s.SavePlan(ctx,Plan{Kind:"workout",Title:"Strength",Sessions:[]Session{{ID:"monday",Date:"2020-01-06",Title:"Squat"}}},0)
 if err!=nil{t.Fatal(err)}
 if p.Revision!=1{t.Fatal("first revision must be 1")}
 if _,err=s.SavePlan(ctx,p,0);!errors.Is(err,ErrConflict){t.Fatalf("expected conflict: %v",err)}
 pending,err:=s.Pending(ctx,p.ID,time.Now())
 if err!=nil||len(pending)!=1||pending[0].State!="unconfirmed"{t.Fatalf("pending: %+v %v",pending,err)}
 m,_:=json.Marshal(map[string]string{"plan_id":p.ID,"session_id":"monday"})
 _,err=s.Add(ctx,Record{Domain:"workout",Metric:"session",Value:45,Unit:"minutes",At:time.Now(),Source:"manual",Method:"reported",Metadata:m})
 if err!=nil{t.Fatal(err)}
 pending,err=s.Pending(ctx,p.ID,time.Now())
 if err!=nil||len(pending)!=0{t.Fatalf("logged session remains pending: %+v %v",pending,err)}
 p.Title="Strength II"
 changed,err:=s.SavePlan(ctx,p,p.Revision)
 if err!=nil||changed.Revision!=2{t.Fatalf("versioning failed: %+v %v",changed,err)}
}
func TestBatchAtomicAndValidation(t *testing.T){
 ctx:=context.Background();s:=testStore(t)
 good:=Record{Domain:"nutrition",Metric:"protein",Value:25,Unit:"g",At:time.Now(),Source:"label",Method:"measured"}
 bad:=good;bad.Metric="Calories not valid"
 if _,err:=s.AddMany(ctx,[]Record{good,bad});err==nil{t.Fatal("invalid batch accepted")}
 list,err:=s.List(ctx,"nutrition","",10,false)
 if err!=nil||len(list)!=0{t.Fatalf("partial batch write: %+v %v",list,err)}
 good2:=good;good2.Metric="vitamin_c";good2.Value=50;good2.Unit="mg"
 out,err:=s.AddMany(ctx,[]Record{good,good2})
 if err!=nil||len(out)!=2{t.Fatalf("valid batch failed: %v",err)}
}
func TestRestrictiveDatabasePermissions(t *testing.T){
 path:=filepath.Join(t.TempDir(),"unsafe.db")
 if err:=os.WriteFile(path,[]byte{},0644);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=Open(path);err==nil{t.Fatal("world-readable database accepted")}
}

func TestReschedulingRequiresConfirmedMissedSession(t *testing.T) {
 ctx:=context.Background();s:=testStore(t)
 p,err:=s.SavePlan(ctx,Plan{Kind:"workout",Title:"Running",Sessions:[]Session{{ID:"session-a",Date:"2020-01-02",Title:"Run"}}},0)
 if err!=nil{t.Fatal(err)}
 if _,err=s.RescheduleConfirmedSession(ctx,p.ID,"session-a","2020-01-03",p.Revision);err==nil{t.Fatal("unconfirmed workout was rescheduled")}
 if err=s.DecideSession(ctx,p.ID,"session-a","completed");err!=nil{t.Fatal(err)}
 if _,err=s.RescheduleConfirmedSession(ctx,p.ID,"session-a","2020-01-03",p.Revision);err==nil{t.Fatal("completed workout was rescheduled")}
 if err=s.DecideSession(ctx,p.ID,"session-a","missed");err!=nil{t.Fatal(err)}
 moved,err:=s.RescheduleConfirmedSession(ctx,p.ID,"session-a","2020-01-03",p.Revision)
 if err!=nil{t.Fatal(err)}
 if moved.Sessions[0].Date!="2020-01-03"||moved.Revision!=2{t.Fatalf("reschedule failed: %+v",moved)}
 pending,err:=s.Pending(ctx,p.ID,time.Now())
 if err!=nil||len(pending)!=1||pending[0].State!="unconfirmed"{t.Fatalf("rescheduled session must require new completion confirmation: %+v %v",pending,err)}
 if _,err=s.RescheduleConfirmedSession(ctx,p.ID,"session-a","2020-01-04",p.Revision);!errors.Is(err,ErrConflict){t.Fatalf("stale edit not rejected: %v",err)}
}
