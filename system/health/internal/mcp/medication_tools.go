package mcp

import(
 "context"
 "encoding/json"
 "errors"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
func(s *Server)registerMedicationTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_medication_save","Save a medication regimen with precise dose, route, time zone, schedules and revision history. This records clinician instructions and does not propose medication changes.",false,false,
 []string{"medication","dose","unit","route","times","timezone","start_date","status","expected_revision"},
 map[string]any{
 "id":txt("Existing regimen ID for edit"),
 "medication":txt("Medication name"),
 "dose":field("number","Numerical dose per scheduled administration"),
 "unit":txt("Dose unit, e.g. mg"),
 "route":txt("Administration route"),
 "times":map[string]any{"type":"array","items":map[string]any{"type":"string"},"description":"Local 24-hour HH:MM clock times"},
 "timezone":txt("IANA time zone"),
 "start_date":txt("Regimen start YYYY-MM-DD"),"end_date":txt("Optional end YYYY-MM-DD"),
 "status":selectField("Current status","active","paused","completed"),
 "instructions":txt("Documented instructions, not AI-prescribed dosing"),
 "expected_revision":field("integer","0 for creation; current revision for edit")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var m health.MedicationRegimen
  if err:=decodeFields(a,&m);err!=nil{return nil,err}
  var revision int
  if json.Unmarshal(a["expected_revision"],&revision)!=nil{return nil,errors.New("expected_revision must be an integer")}
  saved,err:=s.health.SaveMedication(ctx,m,revision);if err!=nil{return nil,err}
  return send(saved)
 })
 s.add("health_medication_list","List recorded medication schedules. Medication changes require the user's review and, when indicated, clinical advice.",true,false,nil,
 map[string]any{"include_inactive":field("boolean","Include paused and completed regimens")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  all:=false
  if len(a["include_inactive"])>0&&json.Unmarshal(a["include_inactive"],&all)!=nil{return nil,errors.New("include_inactive must be boolean")}
  result,err:=s.health.ListMedications(ctx,all);if err!=nil{return nil,err}
  return send(result)
 })
 s.add("health_medication_get","Read one medication regimen.",true,false,[]string{"id"},map[string]any{"id":txt("Regimen ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  result,err:=s.health.GetMedication(ctx,id);if err!=nil{return nil,err}
  return send(result)
 })
 s.add("health_medication_history","Read all saved medication regimen revisions.",true,false,[]string{"id"},map[string]any{"id":txt("Regimen ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  result,err:=s.health.MedicationHistory(ctx,id);if err!=nil{return nil,err}
  return send(result)
 })
 s.add("health_medication_dose_log","Log a confirmed medication administration or an explicitly acknowledged skip/uncertainty. Missing logs must never be classified automatically as missed doses.",false,false,
 []string{"regimen_id","scheduled_for","outcome","source"},
 map[string]any{"regimen_id":txt("Regimen ID"),"scheduled_for":txt("RFC3339 planned dose timestamp"),
 "taken_at":txt("RFC3339 actual administration time for taken doses"),
 "outcome":selectField("Reported dose outcome","taken","skipped","uncertain"),
 "source":txt("Manual or device source"),
 "notes":txt("Additional context")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var d health.MedicationDose
  if err:=decodeFields(a,&d);err!=nil{return nil,err}
  saved,err:=s.health.LogMedicationDose(ctx,d);if err!=nil{return nil,err}
  return send(saved)
 })
 s.add("health_medication_dose_list","Read logged doses for a medication regimen. Absence of a dose record is unknown rather than nonadherence.",true,false,
 []string{"regimen_id"},map[string]any{"regimen_id":txt("Regimen ID"),"limit":field("integer","1 to 500, default 100")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"regimen_id");if err!=nil{return nil,err}
  n,err:=boundedInt(a,"limit",100,1,500);if err!=nil{return nil,err}
  items,err:=s.health.ListMedicationDoses(ctx,id,n);if err!=nil{return nil,err}
  return send(items)
 })
}
