package health

import(
 "context"
 "testing"
 "time"
)
func TestImportRecordsIsAtomicAndIdempotent(t *testing.T){
 ctx:=context.Background();s:=testStore(t)
 at,_:=time.Parse(time.RFC3339,"2026-11-05T16:00:00Z")
 input:=[]Record{{Domain:"biometric",Metric:"weight",Value:80,Unit:"kg",At:at,
  Source:"fhir_import",SourceID:"external1",Method:"reported"}}
 imported,existing,err:=s.ImportRecords(ctx,input)
 if err!=nil||imported!=1||existing!=0{t.Fatalf("import: %d %d %v",imported,existing,err)}
 imported,existing,err=s.ImportRecords(ctx,input)
 if err!=nil||imported!=0||existing!=1{t.Fatalf("duplicate: %d %d %v",imported,existing,err)}
 bad:=append([]Record{},input...)
 bad[0].SourceID="external2"
 bad=append(bad,Record{Domain:"BAD-DOMAIN",Metric:"weight",Value:70,Unit:"kg",At:at,
  Source:"fhir_import",SourceID:"external3",Method:"reported"})
 if _,_,err=s.ImportRecords(ctx,bad);err==nil{t.Fatal("invalid import accepted")}
 results,err:=s.ExportRecords(ctx,"biometric","weight",100,0,false)
 if err!=nil||len(results)!=1{t.Fatalf("partial import: %+v %v",results,err)}
 csv,err:=SimpleExportCSV(results)
 if err!=nil||len(csv)==0{t.Fatalf("CSV export: %q %v",csv,err)}
}
