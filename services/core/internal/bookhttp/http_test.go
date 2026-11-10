package bookhttp

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func serve(mux http.Handler,method,path,body string)*httptest.ResponseRecorder{
 response:=httptest.NewRecorder()
 request:=httptest.NewRequest(method,path,strings.NewReader(body))
 mux.ServeHTTP(response,request)
 return response
}

func TestBookRoutesPreserveRequestsRevisionsAndPersistentState(t *testing.T){
 path:=filepath.Join(t.TempDir(),"state.json")
 database,err:=store.Open(path,"UTC")
 if err!=nil{t.Fatal(err)}
 mux:=http.NewServeMux()
 Register(mux,database,nil)
 requestJSON:=`{"work_id":"/works/OL123W","title":"A Book","format":"ebook"}`
 added:=serve(mux,"POST","/v1/books/requests",requestJSON)
 if added.Code!=http.StatusCreated{t.Fatalf("book request create: %d %s",added.Code,added.Body.String())}
 var book store.BookRequest
 if err:=json.Unmarshal(added.Body.Bytes(),&book);err!=nil{t.Fatal(err)}
 if book.ID==""||book.Revision!=1||book.Status!="requested"{t.Fatal("unexpected book record",book)}
 repeated:=serve(mux,"POST","/v1/books/requests",requestJSON)
 if repeated.Code!=http.StatusConflict{t.Fatalf("duplicate book request: %d",repeated.Code)}
 listed:=serve(mux,"GET","/v1/books/requests","")
 if listed.Code!=http.StatusOK||!strings.Contains(listed.Body.String(),book.ID){
  t.Fatalf("book request disappeared from listing: %d",listed.Code)
 }
 statusPath:="/v1/books/requests/"+book.ID
 stale:=serve(mux,"PUT",statusPath,`{"status":"fulfilled","expected_revision":2}`)
 if stale.Code!=http.StatusConflict{t.Fatalf("stale revision accepted: %d",stale.Code)}
 approved:=serve(mux,"PUT",statusPath,`{"status":"fulfilled","expected_revision":1}`)
 if approved.Code!=http.StatusOK{t.Fatalf("valid book status update: %d %s",approved.Code,approved.Body.String())}
 reopened,err:=store.Open(path,"UTC")
 if err!=nil{t.Fatal(err)}
 records:=reopened.ListBookRequests("")
 if len(records)!=1||records[0].Status!="fulfilled"||records[0].Revision!=2{
  t.Fatal("book record did not survive reopening the same state file")
 }
}

func TestUnconfiguredBookDiscoveryFailsWithoutDisablingRequests(t *testing.T){
 database,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 mux:=http.NewServeMux()
 Register(mux,database,nil)
 for _,path:=range []string{"/v1/books/search?q=Example","/v1/books/lending?isbn=9781234567897"}{
  if w:=serve(mux,"GET",path,"");w.Code!=http.StatusServiceUnavailable{
   t.Fatalf("missing discovery backend exposed %s: %d",path,w.Code)
  }
 }
 if w:=serve(mux,"GET","/v1/books/requests","");w.Code!=http.StatusOK{
  t.Fatalf("request list incorrectly depends on discovery: %d",w.Code)
 }
 for _,path:=range []string{"/mcp","/ui/session","/v1/tools"}{
  if w:=serve(mux,"GET",path,"");w.Code!=http.StatusNotFound{
   t.Fatalf("book domain exposed a foreign interface %s: %d",path,w.Code)
  }
 }
}
