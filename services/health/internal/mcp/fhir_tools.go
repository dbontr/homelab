package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
const fhirMetricSystem="urn:personal-health-mcp:metric"

type fhirCoding struct{
 System string `json:"system"`
 Code string `json:"code"`
 Display string `json:"display,omitempty"`
}
type fhirObservation struct{
 ResourceType string `json:"resourceType"`
 ID string `json:"id"`
 Status string `json:"status"`
 Code struct{
  Coding []fhirCoding `json:"coding"`
 } `json:"code"`
 EffectiveDateTime string `json:"effectiveDateTime"`
 ValueQuantity struct{
  Value *float64 `json:"value"`
  Unit string `json:"unit"`
 } `json:"valueQuantity"`
 Note []struct{Text string `json:"text"`} `json:"note,omitempty"`
}
type fhirBundle struct{
 ResourceType string `json:"resourceType"`
 Type string `json:"type"`
 Entry []struct{Resource fhirObservation `json:"resource"`} `json:"entry"`
}
func exportFHIR(records []health.Record) fhirBundle {
 result:=fhirBundle{ResourceType:"Bundle",Type:"collection"}
 result.Entry=make([]struct{Resource fhirObservation `json:"resource"`},0,len(records))
 for _,r:=range records{
  item:=fhirObservation{
   ResourceType:"Observation",ID:r.ID,Status:"final",
   EffectiveDateTime:r.At.Format(time.RFC3339Nano),
  }
  if r.Method=="estimated"{item.Status="preliminary"}
  item.Code.Coding=[]fhirCoding{{System:fhirMetricSystem,Code:r.Domain+"."+r.Metric,
   Display:r.Metric}}
  value:=r.Value;item.ValueQuantity.Value=&value;item.ValueQuantity.Unit=r.Unit
  item.Note=[]struct{Text string `json:"text"`}{{Text:"Provenance: "+r.Method+"; source: "+r.Source}}
  result.Entry=append(result.Entry,struct{Resource fhirObservation `json:"resource"`}{Resource:item})
 }
 return result
}
func importFHIR(raw json.RawMessage)([]health.Record,error){
 var bundle fhirBundle
 if err:=json.Unmarshal(raw,&bundle);err!=nil{return nil,errors.New("bundle must be a valid FHIR JSON object")}
 if bundle.ResourceType!="Bundle"||bundle.Type!="collection"{return nil,errors.New("expected a FHIR collection Bundle")}
 if len(bundle.Entry)<1||len(bundle.Entry)>200{return nil,errors.New("bundle must contain 1 to 200 observations")}
 records:=make([]health.Record,0,len(bundle.Entry))
 for _,entry:=range bundle.Entry{
  r:=entry.Resource
  if r.ResourceType!="Observation"||r.ID==""||len(r.ID)>64{
   return nil,errors.New("each bundle entry must be an Observation with a stable ID")
  }
  code:=""
  for _,c:=range r.Code.Coding{if c.System==fhirMetricSystem{code=c.Code;break}}
  pieces:=strings.Split(code,".")
  if len(pieces)!=2||pieces[0]==""||pieces[1]==""{
   return nil,errors.New("only locally coded personal-health-mcp numeric observations can be imported")
  }
  at,err:=time.Parse(time.RFC3339Nano,r.EffectiveDateTime)
  if err!=nil{return nil,fmt.Errorf("observation %s has invalid effectiveDateTime",r.ID)}
  if r.ValueQuantity.Value==nil{return nil,errors.New("FHIR numeric Observation is missing valueQuantity.value")}
  records=append(records,health.Record{
   Domain:pieces[0],Metric:pieces[1],Value:*r.ValueQuantity.Value,Unit:r.ValueQuantity.Unit,
   At:at,Method:"reported",Source:"fhir_import",SourceID:r.ID,
   Metadata:json.RawMessage(`{"origin":"fhir_collection","verification":"not clinically verified"}`),
  })
 }
 return records,nil
}
func(s *Server)registerPortabilityTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_records_export","Export original health measurements as full JSON, CSV or a FHIR R4-compatible numeric Observation collection. Supports pagination; do not expose records without user authorization.",true,false,nil,
 map[string]any{
  "domain":txt("Optional domain filter"),"metric":txt("Optional metric filter"),
  "format":selectField("Output format","json","csv","fhir"),
  "offset":field("integer","Starting record offset, default zero"),
  "limit":field("integer","1 to 500 observations, default 100"),
  "include_history":field("boolean","Include superseded measurements")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  n,err:=boundedInt(a,"limit",100,1,500);if err!=nil{return nil,err}
  offset,err:=boundedInt(a,"offset",0,0,10000000);if err!=nil{return nil,err}
  history:=false
  if len(a["include_history"])>0&&json.Unmarshal(a["include_history"],&history)!=nil{
   return nil,errors.New("include_history must be boolean")
  }
  records,err:=s.health.ExportRecords(ctx,optionalString(a,"domain",""),optionalString(a,"metric",""),n,offset,history)
  if err!=nil{return nil,err}
  format:=optionalString(a,"format","json")
  var data any=records
  switch format{
  case "json":
  case "csv":
   encoded,err:=health.SimpleExportCSV(records);if err!=nil{return nil,err}
   data=encoded
  case "fhir":data=exportFHIR(records)
  default:return nil,errors.New("format must be json, csv or fhir")
  }
  return send(map[string]any{"format":format,"count":len(records),"next_offset":offset+len(records),
   "more_may_exist":len(records)==n,
   "privacy":"Exported records contain sensitive personal health information",
   "data":data})
 })
 s.add("health_fhir_import","Import locally coded numeric Observations from a FHIR collection Bundle. Requires explicit confirmation, validates the entire batch and deduplicates source IDs.",false,false,
 []string{"bundle","confirm"},map[string]any{
  "bundle":map[string]any{"type":"object","description":"FHIR R4 collection Bundle with personal-health-mcp numeric observations","additionalProperties":true},
  "confirm":field("boolean","Must be true after reviewing the import")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  if err:=confirmed(a);err!=nil{return nil,errors.New("FHIR import requires confirm=true")}
  if len(a["bundle"])==0||len(a["bundle"])>3<<20{return nil,errors.New("FHIR bundle size is invalid")}
  records,err:=importFHIR(a["bundle"]);if err!=nil{return nil,err}
  imported,existing,err:=s.health.ImportRecords(ctx,records)
  if err!=nil{return nil,err}
  return send(map[string]any{"imported":imported,"existing":existing,
   "method":"reported","note":"Imports are not clinically authenticated; retain original source documentation"})
 })
}
