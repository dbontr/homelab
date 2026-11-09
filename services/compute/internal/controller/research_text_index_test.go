package controller

import (
 "bytes"
 "context"
 "testing"
)

func TestConsentedTextIndexingUsesImmutableSharedLibrary(t *testing.T){
 db,_,server:=fixture(t)
 project(t,server)
 if _,err:=db.CreateProject("other","Other");err!=nil{t.Fatal(err)}
 if _,err:=db.CreateLibrary("articles","Article library","","research");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkLibrary("demo","articles");err!=nil{t.Fatal(err)}
 original:=[]byte("# Theory\nElectromagnetic fields propagate according to Maxwell equations.")
 digest:=checksum(original)
 if _,err:=db.StoreLibraryObject("articles","theory.md","document",digest,bytes.NewReader(original),1<<20);err!=nil{t.Fatal(err)}
 controller:=&Server{DB:db}
 before,err:=controller.IndexPendingText(context.Background(),4)
 if err!=nil||asInt(before["indexed"])!=0{t.Fatalf("nonconsented object indexed: %+v %v",before,err)}
 perm,err:=db.SetTextIndexPolicy("articles",true,0)
 if err!=nil||asInt(perm["revision"])!=1{t.Fatalf("cannot enable indexing: %+v %v",perm,err)}
 if _,err:=db.SetTextIndexPolicy("articles",false,0);err==nil{t.Fatal("stale policy revision accepted")}
 if _,err:=db.ProjectTextIndexPolicy("other","articles");err==nil{t.Fatal("policy leaked across project")}
 result,err:=controller.IndexPendingText(context.Background(),4)
 if err!=nil||asInt(result["indexed"])!=1{t.Fatalf("text ingestion failed: %+v %v",result,err)}
 hits,err:=db.SearchResearch("demo","electromagnetic",10)
 if err!=nil||len(hits)!=1||hits[0]["origin_digest"]!=digest{
  t.Fatalf("indexed text lacks source provenance: %+v %v",hits,err)
 }
 retry,err:=controller.IndexPendingText(context.Background(),4)
 if err!=nil||asInt(retry["indexed"])!=0{t.Fatalf("source indexed twice: %+v %v",retry,err)}
 if _,err:=db.SetTextIndexPolicy("articles",false,1);err!=nil{t.Fatal(err)}
 extra:=[]byte("Unindexed finding about scalar potentials.")
 if _,err:=db.StoreLibraryObject("articles","extra.txt","document",checksum(extra),bytes.NewReader(extra),1<<20);err!=nil{t.Fatal(err)}
 disabled,err:=controller.IndexPendingText(context.Background(),4)
 if err!=nil||asInt(disabled["indexed"])!=0{t.Fatalf("revoked permission ignored: %+v %v",disabled,err)}
}

func TestTextIndexingRejectsInvalidPayloadWithoutBlockingSubsequentSources(t *testing.T){
 db,_,server:=fixture(t)
 project(t,server)
 if _,err:=db.CreateLibrary("articles","Articles","","research");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkLibrary("demo","articles");err!=nil{t.Fatal(err)}
 if _,err:=db.SetTextIndexPolicy("articles",true,0);err!=nil{t.Fatal(err)}
 invalid:=[]byte{0xff,0xfe,0xfd}
 if _,err:=db.StoreLibraryObject("articles","invalid.txt","document",checksum(invalid),bytes.NewReader(invalid),1<<20);err!=nil{t.Fatal(err)}
 valid:=[]byte("Valid published evidence about harmonic oscillations.")
 if _,err:=db.StoreLibraryObject("articles","valid.txt","document",checksum(valid),bytes.NewReader(valid),1<<20);err!=nil{t.Fatal(err)}
 result,err:=(&Server{DB:db}).IndexPendingText(context.Background(),4)
 if err!=nil||asInt(result["indexed"])!=1||asInt(result["failed"])!=1{
  t.Fatalf("invalid text blocked healthy indexing: %+v %v",result,err)
 }
 failures,err:=db.TextIndexFailures("articles")
 if err!=nil||len(failures)!=1||failures[0]["name"]!="invalid.txt"{
  t.Fatalf("failure not persisted: %+v %v",failures,err)
 }
 again,err:=(&Server{DB:db}).IndexPendingText(context.Background(),4)
 if err!=nil||asInt(again["considered"])!=0{t.Fatalf("immutable invalid text retried endlessly: %+v %v",again,err)}
}
