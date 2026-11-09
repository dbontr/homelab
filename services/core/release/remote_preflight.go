package release

import (
 "context"
 "errors"
 "net/http"
 "time"
)

// ReleasePreparation records a non-consuming owner policy decision.
// Approval remains bound to the exact host, operation and release ID.
type ReleasePreparation struct {
 Host string `json:"host"`
 ReleaseID string `json:"release_id"`
 Operation string `json:"operation"`
 Allowed bool `json:"allowed"`
 Reason string `json:"reason,omitempty"`
 ConfirmationID string `json:"confirmation_id,omitempty"`
 ConfirmationExpiresAt time.Time `json:"confirmation_expires_at,omitempty"`
}

func(a *RemoteReleaseAgent)Preview(ctx context.Context,id,action string)(ReleasePreparation,error){
 prepared:=ReleasePreparation{Host:a.host,ReleaseID:id}
 if !idPattern.MatchString(id){return prepared,errors.New("invalid staged release ID")}
 switch action{
 case "activate":prepared.Operation="infrastructure__release_activate"
 case "revert":prepared.Operation="infrastructure__release_revert"
 default:return prepared,errors.New("unregistered release action")
 }
 request:=struct{
  Host string `json:"host"`
  ReleaseID string `json:"release_id"`
  Action string `json:"action"`
 }{a.host,id,action}
 var decision struct{
  Allowed bool `json:"allowed"`
  Reason string `json:"reason"`
  ConfirmationID string `json:"confirmation_id"`
  ConfirmationExpiresAt time.Time `json:"confirmation_expires_at"`
 }
 if err:=a.call(ctx,http.MethodPost,"/v1/release/preview",request,&decision);err!=nil{return prepared,err}
 prepared.Allowed=decision.Allowed
 prepared.Reason=decision.Reason
 prepared.ConfirmationID=decision.ConfirmationID
 prepared.ConfirmationExpiresAt=decision.ConfirmationExpiresAt
 return prepared,nil
}
