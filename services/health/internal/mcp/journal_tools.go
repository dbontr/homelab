package mcp

import(
 "context"
 "encoding/json"
 "errors"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
func(s *Server)registerJournalTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_journal_save","Write a versioned health note such as a symptom, medication, lab result, injury, digestive issue, mood, or rehabilitation session.",false,false,
 []string{"category","title","occurred_at","expected_revision"},
 map[string]any{
 "id":txt("Existing entry ID for edits"),
 "category":selectField("Health domain","symptom","allergy","medication","supplement","laboratory","injury","rehabilitation","mobility","digestive","oral","reproductive","mental_wellbeing","substance","hydration","nutrition_note","training_note","sleep_note","environment","experiment","recommendation","technique_review"),
 "title":txt("Short title"),
 "occurred_at":txt("RFC3339 observation timestamp"),
 "notes":txt("Details; avoid unsupported clinical diagnoses"),
 "attributes":map[string]any{"type":"object","additionalProperties":true,"description":"Structured data with evidence and units"},
 "source":txt("Origin, defaults to manual"),
 "expected_revision":field("integer","Zero for creation; current revision for edits")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var e health.JournalEntry
  if err:=decodeFields(a,&e);err!=nil{return nil,err}
  if e.Source==""{e.Source="manual"}
  var revision int
  if json.Unmarshal(a["expected_revision"],&revision)!=nil{return nil,errors.New("expected_revision must be an integer")}
  saved,err:=s.health.SaveJournal(ctx,e,revision);if err!=nil{return nil,err}
  return send(saved)
 })
 s.add("health_journal_list","List versioned health notes and structured medical or wellness observations.",true,false,nil,
 map[string]any{"category":txt("Optional health domain"),"limit":field("integer","1 to 200, default 50")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  n,err:=boundedInt(a,"limit",50,1,200);if err!=nil{return nil,err}
  entries,err:=s.health.ListJournal(ctx,optionalString(a,"category",""),n);if err!=nil{return nil,err}
  return send(entries)
 })
 s.add("health_journal_get","Read a complete journal entry.",true,false,[]string{"id"},map[string]any{"id":txt("Entry ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  entry,err:=s.health.GetJournal(ctx,id);if err!=nil{return nil,err}
  return send(entry)
 })
 s.add("health_journal_history","Inspect all revisions of a journal entry.",true,false,[]string{"id"},map[string]any{"id":txt("Entry ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  history,err:=s.health.JournalHistory(ctx,id);if err!=nil{return nil,err}
  return send(history)
 })
}
