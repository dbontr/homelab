package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
type TechniqueAssessment struct{
 Exercise string `json:"exercise"`
 EvidenceID string `json:"evidence_id"`
 Observations []string `json:"observations"`
 Limitations string `json:"limitations"`
 Confidence float64 `json:"confidence"`
}
func(s *Server)registerTechniqueTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_technique_review_record","Store an AI-assisted exercise technique observation linked to an original image or document. Observations are provisional, not a medical or biomechanical diagnosis.",false,false,
 []string{"exercise","evidence_id","observations","limitations","confidence"},
 map[string]any{
 "exercise":txt("Exercise name"),"evidence_id":txt("Stored image or document evidence ID"),
 "observations":map[string]any{"type":"array","items":map[string]any{"type":"string"},"description":"Observable movement details, not inferred diagnoses"},
 "limitations":txt("Visibility and model limitations"),
 "confidence":field("number","Subjective confidence 0 to 1")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var assessment TechniqueAssessment
  if err:=decodeFields(a,&assessment);err!=nil{return nil,err}
  if strings.TrimSpace(assessment.Exercise)==""||strings.TrimSpace(assessment.Limitations)==""||
   len(assessment.Observations)==0||len(assessment.Observations)>50||
   assessment.Confidence<0||assessment.Confidence>1{
   return nil,errors.New("review must include exercise, evidence, observations and bounded confidence")
  }
  if assessment.EvidenceID==""{return nil,errors.New("evidence_id is required")}
  exists,err:=s.health.EvidencePresent(ctx,assessment.EvidenceID)
  if err!=nil{return nil,err}
  if !exists{return nil,errors.New("source evidence does not exist")}
  raw,err:=json.Marshal(assessment);if err!=nil{return nil,err}
  entry,err:=s.health.SaveJournal(ctx,health.JournalEntry{
   Category:"technique_review",Title:assessment.Exercise,
   OccurredAt:time.Now().UTC(),Notes:assessment.Limitations,
   Attributes:raw,Source:"visual_observation",
  },0)
  if err!=nil{return nil,err}
  return send(entry)
 })
}
