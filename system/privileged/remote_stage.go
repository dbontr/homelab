package privileged

import (
 "context"
 "encoding/json"
 "io"
 "net/http"
 "regexp"

 "github.com/dbontr/homelab-new/system/release"
 "github.com/dbontr/homelab-new/system/security"
)

var exactRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)

type hostStager interface { Stage(context.Context, string)(release.Manifest,error) }

// registerReleaseStage installs the exact-commit build endpoint on the
// certificate-authenticated executor. No gateway-selected command is accepted.
func(e *Executor)registerReleaseStage(mux *http.ServeMux){
 mux.HandleFunc("POST /v1/release/stage",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  if !e.cfg.AllowRelease{http.Error(w,"release staging disabled",http.StatusForbidden);return}
  stager,ok:=e.cfg.Releases.(hostStager)
  if !ok{http.Error(w,"release staging unavailable",http.StatusServiceUnavailable);return}
  r.Body=http.MaxBytesReader(w,r.Body,1024)
  decoder:=json.NewDecoder(r.Body)
  decoder.DisallowUnknownFields()
  var input struct {Host string `json:"host"`;Revision string `json:"revision"`}
  if err:=decoder.Decode(&input);err!=nil{http.Error(w,"invalid request",http.StatusBadRequest);return}
  var trailing any
  if err:=decoder.Decode(&trailing);err!=io.EOF{http.Error(w,"invalid request",http.StatusBadRequest);return}
  if input.Host!=e.cfg.Host||!exactRevision.MatchString(input.Revision){http.Error(w,"release target or revision denied",http.StatusForbidden);return}
  if r.Header.Get("Cf-Access-Jwt-Assertion")==""{http.Error(w,"owner identity required",http.StatusForbidden);return}
  args,err:=json.Marshal(input)
  if err!=nil{http.Error(w,"invalid arguments",http.StatusBadRequest);return}
  ctx:=security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion"))
  decision,err:=e.cfg.Auth.Authorize(ctx,"infrastructure__release_stage",input.Host,args)
  if err!=nil{http.Error(w,"independent authorization unavailable",http.StatusServiceUnavailable);return}
  if !decision.Allowed {
   w.Header().Set("Content-Type","application/json")
   _=json.NewEncoder(w).Encode(struct {
    Allowed bool `json:"allowed"`
    Reason string `json:"reason,omitempty"`
    ConfirmationID string `json:"confirmation_id,omitempty"`
   }{Reason:decision.Reason,ConfirmationID:decision.ConfirmationID})
   return
  }
  manifest,err:=stager.Stage(ctx,input.Revision)
  if err!=nil||manifest.Host!=input.Host||manifest.Revision!=input.Revision||!releaseJournalID(manifest.ID){
   http.Error(w,"stage failed or returned invalid manifest",http.StatusServiceUnavailable);return
  }
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(struct {
   Allowed bool `json:"allowed"`
   Manifest release.Manifest `json:"manifest"`
  }{Allowed:true,Manifest:manifest})
 })
}
