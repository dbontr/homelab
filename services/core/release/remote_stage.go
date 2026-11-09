package release

import (
 "context"
 "errors"
 "fmt"
 "net/http"
)

// Stage asks a certificate-authenticated host executor to build a clean,
// exact Git revision. It does not activate the generation on that host.
func (a *RemoteReleaseAgent) Stage(ctx context.Context, revision string) (Manifest, error) {
 var manifest Manifest
 if !revisionPattern.MatchString(revision) { return manifest,errors.New("exact 40-character Git revision required") }
 request:=struct{
  Host string `json:"host"`
  Revision string `json:"revision"`
 }{a.host,revision}
 var reply struct {
  Allowed bool `json:"allowed"`
  Reason string `json:"reason"`
  ConfirmationID string `json:"confirmation_id"`
  Manifest *Manifest `json:"manifest"`
 }
 if err:=a.call(ctx,http.MethodPost,"/v1/release/stage",request,&reply);err!=nil{return manifest,err}
 if !reply.Allowed {
  if reply.ConfirmationID!="" { return manifest,fmt.Errorf("owner confirmation required: %s",reply.ConfirmationID) }
  return manifest,errors.New("independent authorization denied release staging")
 }
 if reply.Manifest==nil || reply.Manifest.Host!=a.host || reply.Manifest.Revision!=revision ||
  !idPattern.MatchString(reply.Manifest.ID) || !checkStorePath(reply.Manifest.System) {
  return manifest,errors.New("remote release stage returned mismatched generation")
 }
 return *reply.Manifest,nil
}
