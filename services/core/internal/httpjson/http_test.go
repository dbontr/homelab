package httpjson

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestJSONErrorAndDecoderContracts(t *testing.T){
 w:=httptest.NewRecorder()
 Error(w,http.StatusForbidden,"operation denied")
 if w.Code!=http.StatusForbidden||
  w.Header().Get("Content-Type")!="application/json"||
  w.Body.String()!="{\"error\":\"operation denied\"}\n"{
  t.Fatal("error response differs from canonical service contract")
 }
 for _,input:=range []string{
  `{"title":"one","unapproved":"two"}`,
  `{"title":"one"}{"title":"two"}`,
  strings.Repeat(" ",maxRequestBody+1)+`{"title":"one"}`,
 }{
  req:=httptest.NewRequest(http.MethodPost,"/v1/tasks",strings.NewReader(input))
  var output struct{Title string `json:"title"`}
  if err:=Decode(req,&output);err==nil{
   t.Fatal("unbounded or ambiguous JSON input accepted")
  }
 }
 req:=httptest.NewRequest(http.MethodPost,"/v1/tasks",strings.NewReader(`{"title":"one"}`))
 var output struct{Title string `json:"title"`}
 if err:=Decode(req,&output);err!=nil||output.Title!="one"{
  t.Fatalf("valid bounded payload was rejected: %v",err)
 }
}
