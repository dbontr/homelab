package security

import (
 "errors"
 "time"
)

const (
 ownerRequestLifetime = 5 * time.Minute
 maxOwnerRequests = 64
)

type ownerRequestState struct {
 principal string
 expires time.Time
 completed bool
}

func validOwnerRequestID(id string) bool {
 if len(id)!=48{return false}
 for i:=0;i<len(id);i++ {
  if (id[i]<'0'||id[i]>'9')&&(id[i]<'a'||id[i]>'f'){return false}
 }
 return true
}

func (a *AdminSessions) pruneOwnerRequests(now time.Time) {
 for id,request:=range a.ownerRequests {
  if !now.Before(request.expires) {delete(a.ownerRequests,id)}
 }
}

// OpenOwnerRequestFor creates a correlation-only reference, bound to the
// verified gateway principal. It never authenticates or authorizes a caller.
func (a *AdminSessions) OpenOwnerRequestFor(principal string) (OwnerRequest,error) {
 if principal==""||len(principal)>1024{return OwnerRequest{},errors.New("verified owner identity required")}
 a.mu.Lock()
 defer a.mu.Unlock()
 if a.faulted||a.ownerRequests==nil{return OwnerRequest{},errors.New("owner request unavailable")}
 now:=a.now().UTC()
 a.pruneOwnerRequests(now)
 if len(a.ownerRequests)>=maxOwnerRequests{return OwnerRequest{},errors.New("too many owner requests")}
 id,err:=randomID()
 if err!=nil{return OwnerRequest{},err}
 a.ownerRequests[id]=ownerRequestState{principal:principal,expires:now.Add(ownerRequestLifetime)}
 return OwnerRequest{RequestID:id,ReferenceCode:id[:12],ExpiresInSeconds:int(ownerRequestLifetime/time.Second)},nil
}

// OwnerRequestStatusFor reveals no existence information to another principal.
func (a *AdminSessions) OwnerRequestStatusFor(principal,id string)(completed,found bool) {
 if principal==""||!validOwnerRequestID(id){return false,false}
 a.mu.Lock()
 defer a.mu.Unlock()
 a.pruneOwnerRequests(a.now().UTC())
 request,ok:=a.ownerRequests[id]
 if !ok||request.principal!=principal{return false,false}
 return request.completed,true
}

// ApproveOwnerRequestFor requires a live, independently authenticated owner
// session bound to the same principal and consumes the request exactly once.
func (a *AdminSessions) ApproveOwnerRequestFor(principal,id string)bool {
 if principal==""||!validOwnerRequestID(id){return false}
 a.mu.Lock()
 defer a.mu.Unlock()
 now:=a.now().UTC()
 a.pruneOwnerRequests(now)
 request,ok:=a.ownerRequests[id]
 if !ok||request.principal!=principal||request.completed||!a.validSession(now)||a.session.principal!=principal {
  return false
 }
 request.completed=true
 a.ownerRequests[id]=request
 return true
}
