package privileged

import (
 "context"
 "encoding/json"
 "io"
 "net/http"

 "github.com/dbontr/homelab-new/system/security"
)

type releasePreviewer interface {
 Preview(context.Context,string,string,json.RawMessage)(security.Authorization,error)
}

type stagedVerifier interface {
 VerifyStaged(string)error
}

// registerReleasePreflight previews the exact operation bound to a staged
// generation. It never executes a host mutation or consumes confirmation.
func(e *Executor)registerReleasePreflight(mux *http.ServeMux){
 mux.HandleFunc("POST /v1/release/preview",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  assertion:=r.Header.Get("Cf-Access-Jwt-Assertion")
  if assertion==""{http.Error(w,"signed owner identity required",403);return}
  if !e.cfg.AllowRelease{http.Error(w,"release administration disabled",403);return}
  preview,ok:=e.cfg.Auth.(releasePreviewer)
  if !ok{http.Error(w,"independent preflight unavailable",503);return}
  verifier,ok:=e.cfg.Releases.(stagedVerifier)
  if !ok{http.Error(w,"release manifest verification unavailable",503);return}
  r.Body=http.MaxBytesReader(w,r.Body,1024)
  decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
  var request struct {
   Host string `json:"host"`
   ReleaseID string `json:"release_id"`
   Action string `json:"action"`
  }
  if err:=decoder.Decode(&request);err!=nil{http.Error(w,"invalid request",400);return}
  var trailing any
  if err:=decoder.Decode(&trailing);err!=io.EOF{http.Error(w,"invalid request",400);return}
  if request.Host!=e.cfg.Host||!releaseJournalID(request.ReleaseID){http.Error(w,"resource not permitted",403);return}
  var operation string
  switch request.Action{
  case "activate":operation="infrastructure__release_activate"
  case "revert":operation="infrastructure__release_revert"
  default:http.Error(w,"unknown release action",400);return
  }
  if err:=verifier.VerifyStaged(request.ReleaseID);err!=nil{
   http.Error(w,"verified staged generation required",403);return
  }
  args,err:=json.Marshal(struct{
   Host string `json:"host"`
   ReleaseID string `json:"release_id"`
  }{request.Host,request.ReleaseID})
  if err!=nil{http.Error(w,"invalid arguments",500);return}
  decision,err:=preview.Preview(security.WithAccessAssertion(r.Context(),assertion),operation,request.Host,args)
  if err!=nil{http.Error(w,"independent authorization unavailable",503);return}
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(decision)
 })
}
