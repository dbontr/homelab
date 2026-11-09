package controller

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestNaturalSemanticQueryNeverIncludesNonconsentedLibraries(t *testing.T){
 db,_,httpServer:=fixture(t)
 project(t,httpServer)
 for _,library:=range []string{"consented","private"}{
  if _,err:=db.CreateLibrary(library,library,"","research");err!=nil{t.Fatal(err)}
  if _,err:=db.LinkLibrary("demo",library);err!=nil{t.Fatal(err)}
  data:=[]byte("document "+library)
  digest:=checksum(data)
  if _,err:=db.StoreLibraryObject(library,library+".txt","document",digest,bytes.NewReader(data),1<<20);err!=nil{t.Fatal(err)}
  source,err:=db.RegisterResearchSource(ResearchSourceRequest{Library:library,Title:library,OriginKind:"object",OriginName:library+".txt"})
  if err!=nil{t.Fatal(err)}
  passage:="research in "+library+" library"
  if _,err:=db.IngestResearchPassages(library,asString(source["id"]),"append",0,[]ResearchPassageInput{{Ordinal:1,Text:passage}});err!=nil{t.Fatal(err)}
  rows,err:=db.SearchResearch("demo","research",10)
  if err!=nil{t.Fatal(err)}
  for _,row:=range rows{
   if asString(row["library"])==library{
    if _,err:=db.PutResearchEmbedding(library,asString(row["id"]),"encoder",checksum([]byte(passage)),[]float64{1,0,0,0,0,0,0,0});err!=nil{t.Fatal(err)}
    break
   }
  }
 }
 if _,err:=db.SetEmbeddingPolicy("consented","encoder",true,0);err!=nil{t.Fatal(err)}
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  _=json.NewEncoder(w).Encode(map[string]any{"model":"encoder","data":[]any{map[string]any{"index":0,"embedding":[]float64{1,0,0,0,0,0,0,0}}}})
 }))
 defer upstream.Close()
 provider,err:=NewEmbeddingProvider(upstream.URL+"/v1/embeddings","encoder","")
 if err!=nil{t.Fatal(err)}
 controller:=&Server{DB:db,Embedder:provider}
 all,err:=db.SemanticResearch("demo","encoder",[]float64{1,0,0,0,0,0,0,0},10)
 if err!=nil||len(all)!=2{t.Fatalf("expected manual semantic results: %v %v",all,err)}
 approved,err:=controller.SemanticResearchQuery(context.Background(),"demo","research",10)
 if err!=nil||len(approved)!=1||asString(approved[0]["library"])!="consented"{
  t.Fatalf("unconsented source leaked into text query: %v %v",approved,err)
 }
}
