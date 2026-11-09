package controller

import (
 "bytes"
 "math"
 "strings"
 "testing"
)

func TestSemanticResearchProjectScopeAndImmutableEvidence(t *testing.T){
 db,_,server:=fixture(t)
 project(t,server)
 if _,err:=db.CreateProject("unlinked","Unlinked");err!=nil{t.Fatal(err)}
 if _,err:=db.CreateLibrary("semantic","Vector corpus","","research");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkLibrary("demo","semantic");err!=nil{t.Fatal(err)}
 data:=[]byte("original document")
 digest:=checksum(data)
 if _,err:=db.StoreLibraryObject("semantic","notes.txt","document",digest,bytes.NewReader(data),1<<20);err!=nil{t.Fatal(err)}
 source,err:=db.RegisterResearchSource(ResearchSourceRequest{Library:"semantic",Title:"Methods",OriginKind:"object",OriginName:"notes.txt"})
 if err!=nil{t.Fatal(err)}
 id:=asString(source["id"])
 if _,err:=db.IngestResearchPassages("semantic",id,"append",0,[]ResearchPassageInput{
  {Ordinal:1,Text:"The first vector is about harmonic analysis."},
  {Ordinal:2,Text:"The second vector concerns an unrelated subject."},
 });err!=nil{t.Fatal(err)}
 passages,err:=db.SearchResearch("demo","vector",10)
 if err!=nil||len(passages)!=2{t.Fatalf("source passages: %v %v",passages,err)}
 var first,second string
 for _,p:=range passages{
  if asInt(p["ordinal"])==1{first=asString(p["id"])}else{second=asString(p["id"])}
 }
 a:=[]float64{1,0,0,0,0,0,0,0}
 b:=[]float64{0,1,0,0,0,0,0,0}
 one:=checksum([]byte("The first vector is about harmonic analysis."))
 two:=checksum([]byte("The second vector concerns an unrelated subject."))
 if _,err:=db.PutResearchEmbedding("semantic",first,"encoder-1",one,a);err!=nil{t.Fatal(err)}
 if _,err:=db.PutResearchEmbedding("semantic",second,"encoder-1",two,b);err!=nil{t.Fatal(err)}
 if _,err:=db.PutResearchEmbedding("semantic",first,"encoder-1",one,a);err!=nil{t.Fatal(err)}
 if _,err:=db.PutResearchEmbedding("semantic",first,"encoder-1",one,b);err==nil{t.Fatal("embedding mutation accepted")}
 if _,err:=db.PutResearchEmbedding("semantic",second,"encoder-1",one,b);err==nil{t.Fatal("mismatched digest accepted")}
 hits,err:=db.SemanticResearch("demo","encoder-1",a,10)
 if err!=nil||len(hits)!=2{t.Fatalf("semantic search: %v %v",hits,err)}
 if hits[0]["id"]!=first||hits[0]["score"].(float64)<0.99 || !strings.Contains(asString(hits[0]["citation_ref"]),id){
  t.Fatalf("semantic ordering or citation incorrect: %+v",hits)
 }
 unlinked,err:=db.SemanticResearch("unlinked","encoder-1",a,10)
 if err!=nil||len(unlinked)!=0{t.Fatalf("private library escaped project: %+v %v",unlinked,err)}
 if err:=db.exec("UPDATE research_passages SET body=? WHERE id=?","tampered",first);err!=nil{t.Fatal(err)}
 if _,err:=db.SemanticResearch("demo","encoder-1",a,10);err==nil{t.Fatal("tampered source text surfaced in semantic search")}
 if err:=db.exec("UPDATE research_passages SET body=? WHERE id=?","The first vector is about harmonic analysis.",first);err!=nil{t.Fatal(err)}
 if _,err:=db.IngestResearchPassages("semantic",id,"replace",1,[]ResearchPassageInput{{Ordinal:1,Text:"New scientific knowledge"}});err!=nil{t.Fatal(err)}
 previous,err:=db.SemanticResearch("demo","encoder-1",a,10)
 if err!=nil||len(previous)!=0{t.Fatalf("superseded embeddings surfaced: %+v %v",previous,err)}
}
func TestSemanticEmbeddingVectorLimits(t *testing.T){
 for _,vector:=range [][]float64{nil,{1,0,0},{0,0,0,0,0,0,0,0},{math.NaN(),0,0,0,0,0,0,0},{math.Inf(1),0,0,0,0,0,0,0}}{
  if _,err:=vectorNorm(vector);err==nil{t.Fatalf("invalid vector accepted: %v",vector)}
 }
 if _,err:=vectorNorm([]float64{1,0,0,0,0,0,0,0});err!=nil{t.Fatal(err)}
}
