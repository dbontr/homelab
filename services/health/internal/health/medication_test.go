package health

import(
 "context"
 "errors"
 "testing"
 "time"
)
func TestMedicationRegimenRevisionsAndLoggedDoses(t *testing.T){
 s:=testStore(t);ctx:=context.Background()
 m,err:=s.SaveMedication(ctx,MedicationRegimen{
  Medication:"Example medication",Dose:5,Unit:"mg",Route:"oral",
  Times:[]string{"08:00","20:00"},Timezone:"America/Chicago",
  StartDate:"2026-11-01",Status:"active",
 },0)
 if err!=nil{t.Fatal(err)}
 if m.Revision!=1{t.Fatal("initial medication revision must be one")}
 m.Instructions="Recorded instructions"
 edited,err:=s.SaveMedication(ctx,m,1)
 if err!=nil||edited.Revision!=2{t.Fatalf("regimen edit: %+v %v",edited,err)}
 if _,err=s.SaveMedication(ctx,m,1);!errors.Is(err,ErrConflict){
  t.Fatalf("stale regimen revision accepted: %v",err)
 }
 historical,err:=s.MedicationHistory(ctx,m.ID)
 if err!=nil||len(historical)!=2{t.Fatalf("medication history: %+v %v",historical,err)}
 scheduled,_:=time.Parse(time.RFC3339,"2026-11-05T14:00:00Z")
 if _,err=s.LogMedicationDose(ctx,MedicationDose{RegimenID:m.ID,
  ScheduledFor:scheduled,Outcome:"taken",Source:"manual"});err==nil{
  t.Fatal("taken dose recorded with no actual taken_at")
 }
 got,err:=s.LogMedicationDose(ctx,MedicationDose{RegimenID:m.ID,
  ScheduledFor:scheduled,TakenAt:&scheduled,Outcome:"taken",Source:"manual"})
 if err!=nil||got.ID==""{t.Fatalf("dose logging: %+v %v",got,err)}
 if _,err=s.LogMedicationDose(ctx,MedicationDose{RegimenID:m.ID,
  ScheduledFor:scheduled,TakenAt:&scheduled,Outcome:"taken",Source:"manual"});!errors.Is(err,ErrConflict){
  t.Fatalf("duplicate dose not rejected: %v",err)
 }
 recorded,err:=s.ListMedicationDoses(ctx,m.ID,25)
 if err!=nil||len(recorded)!=1||recorded[0].Outcome!="taken"{
  t.Fatalf("dose history: %+v %v",recorded,err)
 }
}
func TestMedicationSchedulesRequireValidTimezoneAndClock(t *testing.T){
 s:=testStore(t)
 m:=MedicationRegimen{Medication:"Example",Dose:1,Unit:"mg",Route:"oral",
  Times:[]string{"27:00"},Timezone:"America/Chicago",StartDate:"2026-11-01",Status:"active"}
 if _,err:=s.SaveMedication(context.Background(),m,0);err==nil{t.Fatal("invalid schedule accepted")}
 m.Times=[]string{"08:00"}
 m.Timezone="Not/A_Timezone"
 if _,err:=s.SaveMedication(context.Background(),m,0);err==nil{t.Fatal("invalid IANA timezone accepted")}
}
