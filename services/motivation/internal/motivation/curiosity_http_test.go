package motivation

import (
 "bytes"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
)

func TestCuriosityHTTPAuthorizationRevisionAndPause(t *testing.T) {
 engine,err:=Open(filepath.Join(t.TempDir(),"state.json"))
 if err!=nil {t.Fatal(err)}
 defer engine.Close()
 handler,err:=NewHandler(engine,strings.Repeat("x",48))
 if err!=nil{t.Fatal(err)}
 send:=func(method,path,payload,auth string)*httptest.ResponseRecorder{
  t.Helper()
  req:=httptest.NewRequest(method,path,bytes.NewBufferString(payload))
  req.Header.Set("Content-Type","application/json")
  if auth!=""{req.Header.Set("Authorization",auth)}
  w:=httptest.NewRecorder()
  handler.ServeHTTP(w,req)
  return w
 }
 path:="/v1/motivation/curiosity/lemma"
 payload:=`{"expected_revision":0,"enabled":true,"paused":false,"source_kind":"task","source_id":"task_1","question":"What fails at the boundary?","worthwhile_outcome":"A proof or counterexample","first_action":"Evaluate one boundary case","expected_evidence":"A written derivation"}`
 if got:=send("PUT",path,payload,"");got.Code!=http.StatusUnauthorized{t.Fatalf("anonymous write %d",got.Code)}
 if got:=send("GET",path,"","");got.Code!=http.StatusUnauthorized{t.Fatalf("anonymous read %d",got.Code)}
 auth:="Bearer "+strings.Repeat("x",48)
 if got:=send("PUT",path,payload,auth);got.Code!=http.StatusOK{t.Fatalf("owner write %d %s",got.Code,got.Body.String())}
 if got:=send("PUT",path,payload,auth);got.Code!=http.StatusConflict{t.Fatalf("stale write %d",got.Code)}
 if got:=send("GET",path,"",auth);got.Code!=http.StatusConflict{t.Fatalf("globally disabled motivation leaked curiosity %d",got.Code)}
 if got:=send("PUT",path,`{"expected_revision":1,"enabled":true,"paused":false,"source_kind":"task","source_id":"task_1","question":"What fails at the boundary?","worthwhile_outcome":"A proof or counterexample","first_action":"Evaluate one boundary case","expected_evidence":"A written derivation","unknown":"extra"}`,auth);got.Code!=http.StatusBadRequest{t.Fatalf("unknown field admitted %d",got.Code)}
 if got:=send("PUT",path,`{"expected_revision":1,"enabled":false,"paused":true,"source_kind":"task","source_id":"task_1","question":"What fails at the boundary?","worthwhile_outcome":"A proof or counterexample","first_action":"Evaluate one boundary case","expected_evidence":"A written derivation"}`,auth);got.Code!=http.StatusOK{t.Fatalf("pause %d",got.Code)}
}
