package controller

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
)

func TestEmbeddingProviderValidatesResponseAndTransport(t *testing.T){
 for _,url:=range []string{"http://example.com/v1/embeddings","https://example.com/v1/embeddings?key=secret","https://example.com/redirect"}{
  if _,err:=NewEmbeddingProvider(url,"encoder","token");err==nil{t.Fatalf("unsafe endpoint accepted: %s",url)}
 }
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1)
  if r.Method!="POST"||r.URL.Path!="/v1/embeddings"||r.Header.Get("Authorization")!="Bearer private-token"{
   t.Errorf("unexpected embedding HTTP request: %s %s",r.Method,r.URL)
   http.Error(w,"invalid",403);return
  }
  var args struct{Model string `json:"model"`;Input string `json:"input"`}
  if err:=json.NewDecoder(r.Body).Decode(&args);err!=nil{t.Error(err);return}
  if args.Model!="encoder"||args.Input!="scholarly text"{t.Errorf("wrong embedding request: %+v",args)}
  _=json.NewEncoder(w).Encode(map[string]any{"model":"encoder","data":[]any{
   map[string]any{"index":0,"embedding":[]float64{1,0,0,0,0,0,0,0}},
  }})
 }))
 defer upstream.Close()
 client,err:=NewEmbeddingProvider(upstream.URL+"/v1/embeddings","encoder","private-token")
 if err!=nil{t.Fatal(err)}
 vector,err:=client.Embed(context.Background(),"scholarly text")
 if err!=nil||len(vector)!=8||calls.Load()!=1{t.Fatalf("invalid embedding: %v %v",vector,err)}
 if _,err:=client.Embed(context.Background(),"");err==nil{t.Fatal("empty embedding input accepted")}
}

func TestAutomaticEmbeddingIsOwnerAuthorizedAndRevocable(t *testing.T){
 db,_,httpServer:=fixture(t)
 project(t,httpServer)
 if _,err:=db.CreateLibrary("automated","Research","","research");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkLibrary("demo","automated");err!=nil{t.Fatal(err)}
 sourceBytes:=[]byte("original research source")
 digest:=checksum(sourceBytes)
 if _,err:=db.StoreLibraryObject("automated","paper.txt","document",digest,bytes.NewReader(sourceBytes),1<<20);err!=nil{t.Fatal(err)}
 source,err:=db.RegisterResearchSource(ResearchSourceRequest{Library:"automated",Title:"Study",OriginKind:"object",OriginName:"paper.txt"})
 if err!=nil{t.Fatal(err)}
 id:=asString(source["id"])
 if _,err:=db.IngestResearchPassages("automated",id,"append",0,[]ResearchPassageInput{
  {Ordinal:1,Text:"Research methods and reproducibility."},
 });err!=nil{t.Fatal(err)}
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1)
  _=json.NewEncoder(w).Encode(map[string]any{"model":"encoder","data":[]any{map[string]any{"index":0,"embedding":[]float64{1,0,0,0,0,0,0,0}}}})
 }))
 defer upstream.Close()
 client,err:=NewEmbeddingProvider(upstream.URL+"/v1/embeddings","encoder","")
 if err!=nil{t.Fatal(err)}
 service:=&Server{DB:db,Embedder:client}
 result,err:=service.IndexPendingResearch(context.Background(),5)
 if err!=nil||asInt(result["embedded"])!=0||calls.Load()!=0{t.Fatalf("index bypassed consent: %+v %v",result,err)}
 if _,err:=service.SemanticResearchQuery(context.Background(),"demo","methods",5);err==nil{t.Fatal("query sent without policy")}
 policy,err:=db.SetEmbeddingPolicy("automated","encoder",true,0)
 if err!=nil||asInt(policy["revision"])!=1{t.Fatalf("enable: %+v %v",policy,err)}
 if _,err:=db.SetEmbeddingPolicy("automated","encoder",true,0);err==nil{t.Fatal("stale consent revision accepted")}
 result,err=service.IndexPendingResearch(context.Background(),5)
 if err!=nil||asInt(result["embedded"])!=1||calls.Load()!=1{t.Fatalf("index after consent: %+v %v",result,err)}
 hits,err:=service.SemanticResearchQuery(context.Background(),"demo","methods",5)
 if err!=nil||len(hits)!=1||!strings.HasPrefix(asString(hits[0]["citation_ref"]),"source:"){
  t.Fatalf("semantic query missing provenance: %v %v",hits,err)
 }
 _,err=db.DisableEmbeddingPolicy("automated",1)
 if err!=nil{t.Fatal(err)}
 if _,err:=service.SemanticResearchQuery(context.Background(),"demo","methods",5);err==nil{t.Fatal("revoked text query accepted")}
 if _,err:=db.PutResearchEmbedding("automated","invalid","encoder",digest,[]float64{1,0,0,0,0,0,0,0});err==nil{
  t.Fatal("unknown source passage accepted")
 }
 if _,err:=db.SetEmbeddingPolicy("automated","encoder",true,2);err!=nil{t.Fatal(err)}
 if _,err:=db.IngestResearchPassages("automated",id,"replace",1,[]ResearchPassageInput{{Ordinal:1,Text:"Independent evidence."}});err!=nil{t.Fatal(err)}
 result,err=service.IndexPendingResearch(context.Background(),5)
 if err!=nil||asInt(result["embedded"])!=1{t.Fatalf("active epoch was not indexed: %+v %v",result,err)}
 current,err:=db.SemanticResearch("demo","encoder",[]float64{1,0,0,0,0,0,0,0},5)
 if err!=nil||len(current)!=1||!strings.Contains(asString(current[0]["excerpt"]),"Independent"){
  t.Fatalf("superseded evidence surfaced: %v %v",current,err)
 }
}

func TestAutoEmbeddingWriteFencesPolicyRevocation(t *testing.T){
 db,_,httpServer:=fixture(t)
 project(t,httpServer)
 if _,err:=db.CreateLibrary("revoked","Consent corpus","","research");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkLibrary("demo","revoked");err!=nil{t.Fatal(err)}
 data:=[]byte("original")
 digest:=checksum(data)
 if _,err:=db.StoreLibraryObject("revoked","test.txt","document",digest,bytes.NewReader(data),1<<20);err!=nil{t.Fatal(err)}
 source,err:=db.RegisterResearchSource(ResearchSourceRequest{Library:"revoked",Title:"Evidence",OriginKind:"object",OriginName:"test.txt"})
 if err!=nil{t.Fatal(err)}
 if _,err:=db.IngestResearchPassages("revoked",asString(source["id"]),"append",0,[]ResearchPassageInput{{Ordinal:1,Text:"Validated evidence."}});err!=nil{t.Fatal(err)}
 pending,err:=db.pendingResearchEmbeddings("encoder",5)
 if err!=nil||len(pending)!=0{t.Fatal("without opt-in, no passage may be exported")}
 if _,err:=db.SetEmbeddingPolicy("revoked","encoder",true,0);err!=nil{t.Fatal(err)}
 pending,err=db.pendingResearchEmbeddings("encoder",5)
 if err!=nil||len(pending)!=1{t.Fatalf("no approved passage: %+v %v",pending,err)}
 if _,err:=db.DisableEmbeddingPolicy("revoked",1);err!=nil{t.Fatal(err)}
 if err:=db.putConsentedEmbedding("revoked",pending[0].Passage,"encoder",pending[0].Digest,[]float64{1,0,0,0,0,0,0,0});err==nil{
  t.Fatal("revocation between network request and write ignored")
 }
}
