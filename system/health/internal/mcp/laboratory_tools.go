package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "time"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
type LabAnalyte struct{
 Metric string `json:"metric"`
 Name string `json:"name"`
 Value float64 `json:"value"`
 Unit string `json:"unit"`
 ReferenceLow *float64 `json:"reference_low,omitempty"`
 ReferenceHigh *float64 `json:"reference_high,omitempty"`
 Flag string `json:"flag,omitempty"`
}
type LabPanel struct{
 PanelID string `json:"panel_id"`
 Laboratory string `json:"laboratory"`
 ObservedAt time.Time `json:"observed_at"`
 EvidenceID string `json:"evidence_id,omitempty"`
 Specimen string `json:"specimen,omitempty"`
 Method string `json:"method"`
 Analytes []LabAnalyte `json:"analytes"`
}
func(s *Server)recordLabPanel(ctx context.Context,p LabPanel)(any,error){
 if p.Laboratory==""||len(p.Laboratory)>200||p.ObservedAt.IsZero()||
  len(p.Analytes)==0||len(p.Analytes)>150{
  return nil,errors.New("laboratory, observation timestamp and 1 to 150 analytes required")
 }
 if p.Method!="reported"&&p.Method!="measured"{
  return nil,errors.New("method must be reported (transcribed) or measured (directly imported)")
 }
 if p.PanelID==""||len(p.PanelID)>128||len(p.Specimen)>128{
  return nil,errors.New("stable panel_id required (up to 128 characters)")
 }
 if p.EvidenceID!=""{
  present,err:=s.health.EvidencePresent(ctx,p.EvidenceID)
  if err!=nil{return nil,err}
  if !present{return nil,errors.New("evidence ID does not reference stored evidence")}
 }
 records:=make([]health.Record,0,len(p.Analytes))
 seen:=map[string]bool{}
 for _,a:=range p.Analytes{
  if a.Metric==""||a.Name==""||a.Unit==""||len(a.Name)>256{
   return nil,errors.New("analyte metric, name and unit required")
  }
  if seen[a.Metric] {return nil,fmt.Errorf("duplicate lab analyte: %s",a.Metric)}
  seen[a.Metric]=true
  for _,f:=range []*float64{&a.Value,a.ReferenceLow,a.ReferenceHigh}{
   if f!=nil&&(math.IsNaN(*f)||math.IsInf(*f,0)){return nil,errors.New("lab values must be finite")}
  }
  if a.ReferenceLow!=nil&&a.ReferenceHigh!=nil&&*a.ReferenceLow>*a.ReferenceHigh{
   return nil,fmt.Errorf("invalid reference interval for %s",a.Metric)
  }
  if a.Flag!=""&&a.Flag!="high"&&a.Flag!="low"&&a.Flag!="normal"&&a.Flag!="unknown"{
   return nil,errors.New("analyte flag must be high, low, normal, unknown or empty")
  }
  meta,_:=json.Marshal(map[string]any{"panel_id":p.PanelID,"test_name":a.Name,
   "laboratory":p.Laboratory,"specimen":p.Specimen,
   "reference_low":a.ReferenceLow,"reference_high":a.ReferenceHigh,
   "lab_reported_flag":a.Flag,"evidence_id":p.EvidenceID,
   "note":"Reference intervals are specific to the reported laboratory; values are not diagnostic interpretations"})
  records=append(records,health.Record{
   Domain:"laboratory",Metric:a.Metric,Value:a.Value,Unit:a.Unit,At:p.ObservedAt.UTC(),
   Source:p.Laboratory,Method:p.Method,Metadata:meta,
  })
 }
 saved,err:=s.health.AddMany(ctx,records)
 if err!=nil{return nil,err}
 return map[string]any{"panel_id":p.PanelID,"laboratory":p.Laboratory,
  "observations":saved,"count":len(saved),"method":p.Method},nil
}
func(s *Server)registerLaboratoryTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_laboratory_panel_record","Import one complete numerical laboratory panel with source-specific reference limits. Verify transcription against the original report before saving. This is not a clinical interpretation.",false,false,
 []string{"panel_id","laboratory","observed_at","method","analytes","confirm"},
 map[string]any{
 "panel_id":txt("Stable panel identity"),
 "laboratory":txt("Source laboratory"),
 "observed_at":txt("RFC3339 specimen collection or observation time"),
 "method":selectField("Origin of numbers","reported","measured"),
 "specimen":txt("Specimen type"),
 "evidence_id":txt("Optional original report evidence ID"),
 "analytes":map[string]any{"type":"array","items":map[string]any{
 "type":"object","properties":map[string]any{
 "metric":txt("Canonical lowercase identifier, e.g. hemoglobin_a1c"),
 "name":txt("Laboratory-reported test name"),"value":field("number","Numeric reported result"),
 "unit":txt("Exact laboratory unit"),
 "reference_low":field("number","Optional lower reference limit"),
 "reference_high":field("number","Optional upper reference limit"),
 "flag":selectField("As reported by laboratory","high","low","normal","unknown")},
 "required":[]string{"metric","name","value","unit"}}},
 "confirm":field("boolean","Required after verifying the entered values against the report")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  if err:=confirmed(a);err!=nil{return nil,errors.New("lab import requires confirm=true")}
  var panel LabPanel
  if err:=decodeFields(a,&panel);err!=nil{return nil,err}
  result,err:=s.recordLabPanel(ctx,panel);if err!=nil{return nil,err}
  return send(result)
 })
}
