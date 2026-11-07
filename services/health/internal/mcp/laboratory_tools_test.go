package mcp

import(
 "context"
 "testing"
 "time"

)
func TestLabPanelValidatesBeforeAtomicSave(t *testing.T){
 s:=healthServer(t);ctx:=context.Background()
 timeAt,_:=time.Parse(time.RFC3339,"2026-11-05T16:00:00Z")
 v1,v2:=70.0,100.0
 panel:=LabPanel{PanelID:"panel-1",Laboratory:"Example Lab",ObservedAt:timeAt,Method:"reported",
  Analytes:[]LabAnalyte{
   {Metric:"glucose",Name:"Glucose",Value:90,Unit:"mg/dL",ReferenceLow:&v1,ReferenceHigh:&v2},
   {Metric:"hemoglobin_a1c",Name:"HbA1c",Value:5.4,Unit:"%",ReferenceLow:&v1,ReferenceHigh:&v2},
  }}
 _,err:=s.recordLabPanel(ctx,panel)
 if err!=nil{t.Fatal(err)}
 results,err:=s.health.List(ctx,"laboratory","",10,false)
 if err!=nil||len(results)!=2{t.Fatalf("lab records: %+v %v",results,err)}
 panel.PanelID="panel-2";panel.Analytes[1].Metric="not a valid metric"
 if _,err=s.recordLabPanel(ctx,panel);err==nil{t.Fatal("invalid panel accepted")}
 after,err:=s.health.List(ctx,"laboratory","",10,false)
 if err!=nil||len(after)!=2{t.Fatalf("partial panel import: %+v %v",after,err)}
}
