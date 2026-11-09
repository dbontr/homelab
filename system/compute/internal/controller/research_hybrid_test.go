package controller

import "testing"

func TestHybridRankingPreservesCitationsAndDeduplicates(t *testing.T){
 lexical:=[]map[string]any{
  {"id":"a","citation_ref":"source:a@1:1","excerpt":"A"},
  {"id":"b","citation_ref":"source:b@1:1","excerpt":"B"},
 }
 semantic:=[]map[string]any{
  {"id":"b","citation_ref":"source:b@1:1","excerpt":"B","score":0.8},
  {"id":"c","citation_ref":"source:c@1:1","excerpt":"C","score":0.7},
 }
 out:=fusePassages(lexical,semantic,3)
 if len(out)!=3||out[0]["id"]!="b"||out[0]["citation_ref"]!="source:b@1:1"{
  t.Fatalf("reciprocal ranking lost cited overlap: %+v",out)
 }
 sources:=out[0]["retrieval_sources"].([]string)
 if len(sources)!=2{t.Fatalf("combined source channels missing: %+v",sources)}
}
func TestHybridSearchWorksWithoutEmbeddingProvider(t *testing.T){
 db,_,httpServer:=fixture(t)
 project(t,httpServer)
 srv:=&Server{DB:db}
 if _,err:=srv.HybridResearch(t.Context(),"other","physics",5);err==nil{
  t.Fatal("hybrid bypassed unknown project")
 }
 // Empty lexical results are valid for an existing project; no embedding
 // service is created or falsely reported as available.
 result,err:=srv.HybridResearch(t.Context(),"demo","quantum",5)
 if err!=nil||result["mode"]!="lexical_only"{
  t.Fatalf("degraded retrieval reported as hybrid: %+v %v",result,err)
 }
}
