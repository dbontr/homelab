package privileged

import (
 "context"
 "io"
 "net/http"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

type stageRoundTrip func(*http.Request)(*http.Response,error)
func (f stageRoundTrip)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}

func TestStageClientRequiresSignedOwnerAndValidManifest(t *testing.T){
 revision:=strings.Repeat("b",40)
 id:=strings.Repeat("c",48)
 response:=`{"allowed":true,"manifest":{"host":"saturn","revision":"`+revision+`","id":"`+id+`","system":"/nix/store/test-system"}}`
 client:=&Client{http:&http.Client{Transport:stageRoundTrip(func(r *http.Request)(*http.Response,error){
  if r.Method!=http.MethodPost||r.URL.Path!="/v1/release/stage"{t.Fatal("unexpected staging endpoint")}
  if r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner"{t.Fatal("signed owner assertion missing")}
  if r.Header.Get("Content-Type")!="application/json"{t.Fatal("stage body type missing")}
  raw,err:=io.ReadAll(r.Body)
  if err!=nil||string(raw)!=`{"host":"saturn","revision":"`+revision+`"}`{t.Fatalf("invalid request body: %s %v",raw,err)}
  return &http.Response{StatusCode:200,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(response))},nil
 })}}
 ctx:=security.WithAccessAssertion(context.Background(),"signed-owner")
 result,err:=client.Stage(ctx,"saturn",revision)
 if err!=nil||!result.Allowed||result.Manifest==nil||result.Manifest.ID!=id{t.Fatalf("valid staged result rejected: %v",err)}
 response=`{"allowed":true,"manifest":{"host":"venus","revision":"`+revision+`","id":"`+id+`","system":"/nix/store/test-system"}}`
 if _,err:=client.Stage(ctx,"saturn",revision);err==nil{t.Fatal("wrong-host manifest accepted")}
 response=`{"allowed":false,"reason":"confirmation required"}`
 denied,err:=client.Stage(ctx,"saturn",revision)
 if err!=nil||denied.Allowed||denied.Reason!="confirmation required"{t.Fatalf("denied response lost: %v",err)}
 if _,err:=client.Stage(ctx,"saturn","invalid");err==nil{t.Fatal("invalid revision accepted")}
}
