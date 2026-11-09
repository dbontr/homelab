package security

import (
 "log"
 "net/http"
)

// registerCredentialRotation is reachable only through the owner-only socket.
// Each mutation requires fresh password and TOTP factors.
func(a *AdminSessions)registerCredentialRotation(mux *http.ServeMux){
 mux.HandleFunc("PUT /v1/credentials/{id}",func(w http.ResponseWriter,r *http.Request){
  if a.vault==nil{http.Error(w,"credential vault unavailable",http.StatusServiceUnavailable);return}
  var input struct{
   Provider string `json:"provider"`
   Secret string `json:"secret"`
   Password string `json:"password"`
   TOTP string `json:"totp"`
  }
  if err:=readJSON(r,&input);err!=nil{http.Error(w,"invalid credential request",400);return}
  if err:=a.VerifyOwnerFactors(input.Password,input.TOTP);err!=nil{
   log.Print("security audit credential_rotation denied")
   http.Error(w,"authentication denied",403);return
  }
  secret:=[]byte(input.Secret)
  handle,err:=a.vault.Rotate(r.PathValue("id"),input.Provider,secret)
  for i:=range secret{secret[i]=0}
  if err!=nil{
   log.Print("security audit credential_rotation rejected")
   http.Error(w,"credential rotation unavailable",400);return
  }
  log.Print("security audit credential rotated")
  respondJSON(w,http.StatusOK,handle)
 })
}
