package agentflow

import (
 "context"
 "encoding/json"
 "errors"
 "net"
 "net/url"
 "strings"
)

// CapabilityPlanner plans and continues bounded read-only workflows. The
// implementations only propose steps; the workflow owner enforces policy.
type CapabilityPlanner interface {
 ProposeWorkflowStages(context.Context,string,string,bool)(json.RawMessage,error)
 ContinueWorkflowStages(context.Context,string,string,[]map[string]any,int)(json.RawMessage,error)
}

// ModelRoute binds one declared task purpose to an exact operator-selected
// model. Tokens are supplied from protected service credentials, never Git.
type ModelRoute struct {
 Name string `json:"name"`
 Purpose string `json:"purpose"`
 Endpoint string `json:"endpoint"`
 Model string `json:"model"`
 LocalOnly bool `json:"local_only"`
 Token string `json:"-"`
}

type ModelRouter struct {
 routes map[string]*ModelAuthor
}
func NewModelRouter(specs []ModelRoute)(*ModelRouter,error){
 if len(specs)<1||len(specs)>3{return nil,errors.New("one to three model routes required")}
 router:=&ModelRouter{routes:make(map[string]*ModelAuthor,len(specs))}
 names:=map[string]bool{}
 for _,spec:=range specs{
  if spec.Name==""||len(spec.Name)>64||strings.ContainsAny(spec.Name,"\x00\r\n /\\")||names[spec.Name]{
   return nil,errors.New("invalid or duplicate model route identity")
  }
  names[spec.Name]=true
  if spec.Purpose!="planning"&&spec.Purpose!="continuation"&&spec.Purpose!="evidence"{
   return nil,errors.New("invalid model purpose")
  }
  if router.routes[spec.Purpose]!=nil{return nil,errors.New("duplicate model purpose")}
  if spec.LocalOnly{
   parsed,err:=url.Parse(spec.Endpoint)
   if err!=nil{return nil,errors.New("invalid local model endpoint")}
   host:=parsed.Hostname()
   ip:=net.ParseIP(host)
   if host!="localhost"&&(ip==nil||!ip.IsLoopback()){
    return nil,errors.New("local-only route cannot use a remote provider")
   }
  }
  author,err:=NewModelAuthor(spec.Endpoint,spec.Model,spec.Token)
  if err!=nil{return nil,err}
  router.routes[spec.Purpose]=author
 }
 return router,nil
}

func (r *ModelRouter) ProposeWorkflowStages(ctx context.Context,project,objective string,
 personalRead bool)(json.RawMessage,error){
 if r==nil||r.routes["planning"]==nil{return nil,errors.New("planning model route is unavailable")}
 return r.routes["planning"].ProposeWorkflowStages(ctx,project,objective,personalRead)
}
func (r *ModelRouter) ContinueWorkflowStages(ctx context.Context,project,objective string,
 observations []map[string]any,remaining int)(json.RawMessage,error){
 purpose:="continuation"
 for _,item:=range observations{
  if _,hasEvidence:=item["evidence"];hasEvidence {
   purpose="evidence"
   break
  }
 }
 if r==nil||r.routes[purpose]==nil{
  return nil,errors.New("authorized model purpose is unavailable")
 }
 // No automatic fallback: moving an evidence-bearing request to a different
 // provider would change the owner's declared disclosure boundary.
 return r.routes[purpose].ContinueWorkflowStages(ctx,project,objective,observations,remaining)
}

// AllowsAutomaticContinuation is an owner-visible admission check. It avoids
// persisting plans that are guaranteed to fail at the first model continuation.
func (r *ModelRouter) AllowsAutomaticContinuation(shareEvidence bool)bool{
 return r!=nil&&r.routes["continuation"]!=nil&&(!shareEvidence||r.routes["evidence"]!=nil)
}

func (r *ModelRouter) SupportsEvidence()bool{
 return r!=nil&&r.routes["evidence"]!=nil
}
