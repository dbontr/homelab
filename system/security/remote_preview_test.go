package security

import (
 "context"
 "encoding/json"
 "net/http"
 "testing"
)

func TestAuthenticatedRemotePolicyPreviewUsesOnlyPreviewEndpoint(t *testing.T){
 calls:=0
 cfg,_,_,closeServer:=remoteAuthFixture(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.URL.Path!="/v1/preview"||r.Method!="POST"||r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner"{
   t.Fatal("remote preview did not preserve owner identity or route")
  }
  var request struct{
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments json.RawMessage `json:"arguments"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil||request.Operation!="infrastructure__release_activate"||
   request.Target!="jupiter"||string(request.Arguments)!=`{"host":"jupiter"}`{
   t.Fatal("altered preview scope",request,err)
  }
  _=json.NewEncoder(w).Encode(Authorization{Allowed:false,Reason:"confirmation required",ConfirmationID:"challenge"})
 }))
 defer closeServer()
 client,err:=NewRemoteAuthorizationClient(cfg);if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"jupiter"}`)
 if _,err:=client.Preview(context.Background(),"infrastructure__release_activate","jupiter",args);err==nil||calls!=0{
  t.Fatal("unsigned preview passed remote authorization")
 }
 result,err:=client.Preview(WithAccessAssertion(context.Background(),"signed-owner"),
  "infrastructure__release_activate","jupiter",args)
 if err!=nil||result.Allowed||result.ConfirmationID!="challenge"||calls!=1{
  t.Fatal("invalid remote policy preview",result,err)
 }
}
