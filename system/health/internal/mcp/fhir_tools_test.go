package mcp

import(
 "encoding/json"
 "testing"
 "time"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
func TestFHIRNumericRoundtripAndMissingValue(t *testing.T){
 when,_:=time.Parse(time.RFC3339,"2026-11-05T11:00:00Z")
 source:=[]health.Record{{ID:"observ1",Domain:"biometric",Metric:"weight",Value:80.5,Unit:"kg",
  At:when,Source:"manual",Method:"measured"}}
 bundle:=exportFHIR(source)
 raw,err:=json.Marshal(bundle);if err!=nil{t.Fatal(err)}
 decoded,err:=importFHIR(raw)
 if err!=nil{t.Fatal(err)}
 if len(decoded)!=1||decoded[0].Value!=80.5||decoded[0].Method!="reported"||decoded[0].SourceID!="observ1"{
  t.Fatalf("FHIR import failed: %+v",decoded)
 }
 invalid:=`{"resourceType":"Bundle","type":"collection","entry":[{"resource":{
  "resourceType":"Observation","id":"test","code":{"coding":[{"system":"urn:personal-health-mcp:metric","code":"biometric.weight"}]},
  "effectiveDateTime":"2026-11-05T11:00:00Z","valueQuantity":{"unit":"kg"}}}]}`
 if _,err:=importFHIR([]byte(invalid));err==nil{t.Fatal("accepted missing numeric value")}
}
