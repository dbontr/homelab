package controller

import (
 "bytes"
 "testing"
)

func TestResearchClaimsRequireProjectVisibleEvidence(t *testing.T){
 db,_,server:=fixture(t)
 project(t,server)
 if _,err:=db.CreateProject("other","Other");err!=nil{t.Fatal(err)}
 if _,err:=db.CreateLibrary("evidence","Physics","","research");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkLibrary("demo","evidence");err!=nil{t.Fatal(err)}
 data:=[]byte("immutable paper")
 if _,err:=db.StoreLibraryObject("evidence","paper.txt","document",checksum(data),bytes.NewReader(data),1<<20);err!=nil{t.Fatal(err)}
 source,err:=db.RegisterResearchSource(ResearchSourceRequest{Library:"evidence",Title:"Original evidence",OriginKind:"object",OriginName:"paper.txt"})
 if err!=nil{t.Fatal(err)}
 sourceID:=asString(source["id"])
 passage:="The experiment shows one reproducible effect."
 if _,err:=db.IngestResearchPassages("evidence",sourceID,"append",0,[]ResearchPassageInput{{Ordinal:1,Text:passage}});err!=nil{t.Fatal(err)}
 hits,err:=db.SearchResearch("demo","experiment",5)
 if err!=nil||len(hits)!=1{t.Fatalf("passage: %v %v",hits,err)}
 passageID:=asString(hits[0]["id"])
 claim,err:=db.CreateResearchClaim("demo","The mechanism may be experimentally demonstrable.","hypothesis")
 if err!=nil{t.Fatal(err)}
 claimID:=asString(claim["id"])
 if _,err:=db.AddResearchClaimEvidence("other",claimID,passageID,"supports",1);err==nil{t.Fatal("claim escaped project")}
 if _,err:=db.AddResearchClaimEvidence("demo",claimID,passageID,"supports",2);err==nil{t.Fatal("incorrect revision accepted")}
 linked,err:=db.AddResearchClaimEvidence("demo",claimID,passageID,"supports",1)
 if err!=nil||asInt(linked["revision"])!=2{t.Fatalf("evidence write: %v %v",linked,err)}
 if _,err:=db.AddResearchClaimEvidence("demo",claimID,passageID,"challenges",2);err==nil{t.Fatal("conflicting evidence was silently rewritten")}
 if _,err:=db.AddResearchClaimEvidence("demo",claimID,passageID,"supports",1);err!=nil{t.Fatalf("identical replay was not idempotent: %v",err)}
 read,err:=db.ReadResearchClaim("demo",claimID)
 if err!=nil||read["assessment"]!="supporting_evidence"||asInt(read["supporting_sources"])!=1{
  t.Fatalf("incorrect claim assessment: %+v %v",read,err)
 }
 if _,err:=db.IngestResearchPassages("evidence",sourceID,"replace",1,[]ResearchPassageInput{{Ordinal:1,Text:"Revised and independent observations."}});err!=nil{t.Fatal(err)}
 stale,err:=db.ReadResearchClaim("demo",claimID)
 if err!=nil||stale["assessment"]!="unassessed"||asInt(stale["unavailable_links"])!=1{
  t.Fatalf("superseded evidence remained current: %+v %v",stale,err)
 }
 if _,err:=db.ReadResearchClaim("other",claimID);err==nil{t.Fatal("claim disclosure outside project")}
}
