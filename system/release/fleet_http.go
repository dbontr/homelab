package release

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

// FleetAuthorizer must be the independently managed owner security service.
type FleetAuthorizer interface{
 Authorize(context.Context,string,string,json.RawMessage)(security.Authorization,error)
}

type FleetService struct{
 Controller *FleetOrchestrator
 Authorizer FleetAuthorizer
}

func(s *FleetService)Handler()http.Handler{
 mux:=http.NewServeMux()
 authorize:=func(w http.ResponseWriter,r *http.Request,operation string,args any)bool{
  assertion:=r.Header.Get("Cf-Access-Jwt-Assertion")
  if assertion==""{http.Error(w,"signed owner identity required",http.StatusForbidden);return false}
  payload,err:=json.Marshal(args)
  if err!=nil{http.Error(w,"invalid authorization scope",http.StatusBadRequest);return false}
  decision,err:=s.Authorizer.Authorize(security.WithAccessAssertion(r.Context(),assertion),operation,"fleet",payload)
  if err!=nil{http.Error(w,"independent authorization unavailable",http.StatusServiceUnavailable);return false}
  if !decision.Allowed{
   w.Header().Set("Content-Type","application/json")
   _=json.NewEncoder(w).Encode(decision)
   return false
  }
  return true
 }
 read:=func(w http.ResponseWriter,r *http.Request,dst any)bool{
  r.Body=http.MaxBytesReader(w,r.Body,4096)
  decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
  if err:=decoder.Decode(dst);err!=nil{http.Error(w,"invalid request",400);return false}
  var extra any
  if err:=decoder.Decode(&extra);err!=io.EOF{http.Error(w,"invalid request",400);return false}
  return true
 }
 success:=func(w http.ResponseWriter,value any){
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(map[string]any{"allowed":true,"result":value})
 }
 mux.HandleFunc("POST /v1/stage",func(w http.ResponseWriter,r *http.Request){
  var input struct{Revision string `json:"revision"`}
  if !read(w,r,&input){return}
  if !revisionPattern.MatchString(input.Revision){http.Error(w,"exact source revision required",400);return}
  if !authorize(w,r,"infrastructure__fleet_stage",input){return}
  ctx:=security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion"))
  plan,err:=s.Controller.Stage(ctx,input.Revision)
  if err!=nil{http.Error(w,"fleet staging failed",503);return}
  success(w,plan)
 })
 mux.HandleFunc("POST /v1/activate",func(w http.ResponseWriter,r *http.Request){
  var input struct{PlanID string `json:"plan_id"`}
  if !read(w,r,&input){return}
  if !idPattern.MatchString(input.PlanID){http.Error(w,"invalid plan identity",400);return}
  if !authorize(w,r,"infrastructure__fleet_activate",input){return}
  ctx:=security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion"))
  tx,err:=s.Controller.Activate(ctx,input.PlanID)
  if err!=nil{http.Error(w,"fleet activation incomplete; inspect transaction journal",503);return}
  success(w,tx)
 })
 mux.HandleFunc("POST /v1/recover",func(w http.ResponseWriter,r *http.Request){
  var input struct{TransactionID string `json:"transaction_id"`}
  if !read(w,r,&input){return}
  if !idPattern.MatchString(input.TransactionID){http.Error(w,"invalid transaction identity",400);return}
  if !authorize(w,r,"infrastructure__fleet_recover",input){return}
  ctx:=security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion"))
  tx,err:=s.Controller.Recover(ctx,input.TransactionID)
  if err!=nil{http.Error(w,"fleet recovery failed",503);return}
  success(w,tx)
 })
 mux.HandleFunc("GET /v1/plan",func(w http.ResponseWriter,r *http.Request){
  if !authorize(w,r,"infrastructure__fleet_plan",struct{Resource string `json:"resource"`}{"plan"}){return}
  plan,err:=s.Controller.Plan()
  if errors.Is(err,os.ErrNotExist){success(w,map[string]bool{"staged":false});return}
  if err!=nil{http.Error(w,"fleet plan unavailable",503);return}
  success(w,plan)
 })
 mux.HandleFunc("GET /v1/status",func(w http.ResponseWriter,r *http.Request){
  if !authorize(w,r,"infrastructure__fleet_status",struct{Resource string `json:"resource"`}{"transaction"}){return}
  tx,err:=s.Controller.Fleet.Status()
  if errors.Is(err,os.ErrNotExist){success(w,map[string]bool{"started":false});return}
  if err!=nil{http.Error(w,"fleet journal unavailable",503);return}
  success(w,tx)
 })
 mux.HandleFunc("DELETE /v1/plan/{id}",func(w http.ResponseWriter,r *http.Request){
  id:=r.PathValue("id")
  if !idPattern.MatchString(id){http.Error(w,"invalid plan identity",400);return}
  input:=struct{PlanID string `json:"plan_id"`}{id}
  if !authorize(w,r,"infrastructure__fleet_clear",input){return}
  if err:=s.Controller.Clear(id);err!=nil{http.Error(w,"fleet plan cannot be cleared",409);return}
  success(w,map[string]bool{"cleared":true})
 })
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  if s==nil||s.Authorizer==nil||s.Controller==nil{http.Error(w,"fleet service unavailable",503);return}
  mux.ServeHTTP(w,r)
 })
}

// ServeSocket exposes only the fleet API through a protected local Unix socket.
// Certificate keys are retained by the release daemon, never the MCP process.
func(s *FleetService)ServeSocket(ctx context.Context,path string,clientGroupID int)error{
 if s==nil||s.Controller==nil||s.Authorizer==nil||!filepath.IsAbs(path)||clientGroupID<=0||strings.ContainsRune(path,0){
  return errors.New("invalid fleet socket configuration")
 }
 dir:=filepath.Dir(path)
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0007!=0{return errors.New("fleet runtime directory is not private")}
 if err:=os.Chown(dir,-1,clientGroupID);err!=nil{return err}
 if err:=os.Chmod(dir,0750);err!=nil{return err}
 if _,err:=os.Lstat(path);err==nil{return errors.New("fleet socket already exists")}else if !errors.Is(err,os.ErrNotExist){return err}
 listener,err:=net.Listen("unix",path);if err!=nil{return err}
 defer listener.Close();defer os.Remove(path)
 if err:=os.Chown(path,-1,clientGroupID);err!=nil{return err}
 if err:=os.Chmod(path,0660);err!=nil{return err}
 server:=&http.Server{Handler:s.Handler(),ReadHeaderTimeout:5*time.Second,ReadTimeout:20*time.Second,WriteTimeout:3*time.Hour}
 stopped:=make(chan struct{})
 go func(){select{case <-ctx.Done():_=server.Close();case <-stopped:}}()
 err=server.Serve(listener);close(stopped)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}

// FleetClient forwards the caller's signed owner assertion over local IPC,
// never host TLS credentials or an unscoped command.
type FleetClient struct{http *http.Client}

func NewFleetClient(socket string)(*FleetClient,error){
 if !filepath.IsAbs(socket){return nil,errors.New("fleet client socket must be absolute")}
 transport:=&http.Transport{DisableKeepAlives:true,DialContext:func(ctx context.Context,_,_ string)(net.Conn,error){
  return (&net.Dialer{Timeout:2*time.Second}).DialContext(ctx,"unix",socket)
 }}
 return &FleetClient{http:&http.Client{Transport:transport,Timeout:3*time.Hour}},nil
}

func(c *FleetClient)Call(ctx context.Context,operation string,args json.RawMessage)(json.RawMessage,error){
 var method,path string
 switch operation{
 case "infrastructure__fleet_stage":method,path="POST","/v1/stage"
 case "infrastructure__fleet_activate":method,path="POST","/v1/activate"
 case "infrastructure__fleet_recover":method,path="POST","/v1/recover"
 case "infrastructure__fleet_plan":method,path="GET","/v1/plan"
 case "infrastructure__fleet_status":method,path="GET","/v1/status"
 case "infrastructure__fleet_clear":
  var input struct{PlanID string `json:"plan_id"`}
  if err:=json.Unmarshal(args,&input);err!=nil||!idPattern.MatchString(input.PlanID){return nil,errors.New("invalid fleet plan identity")}
  method,path="DELETE","/v1/plan/"+input.PlanID
 default:return nil,errors.New("unregistered fleet operation")
 }
 var body io.Reader
 if method=="POST"{body=bytes.NewReader(args)}
 request,err:=http.NewRequestWithContext(ctx,method,"http://unix"+path,body)
 if err!=nil{return nil,err}
 request.Header.Set("Cf-Access-Jwt-Assertion",security.AccessAssertion(ctx))
 if method=="POST"{request.Header.Set("Content-Type","application/json")}
 response,err:=c.http.Do(request);if err!=nil{return nil,err}
 defer response.Body.Close()
 if response.StatusCode!=200{return nil,errors.New("fleet service refused request")}
 data,err:=io.ReadAll(io.LimitReader(response.Body,32769))
 if err!=nil||len(data)>32768{return nil,errors.New("fleet response exceeded limit")}
 var envelope struct{Allowed bool `json:"allowed"`;Result json.RawMessage `json:"result"`;Reason string `json:"reason"`;ConfirmationID string `json:"confirmation_id"`}
 if err:=json.Unmarshal(data,&envelope);err!=nil{return nil,errors.New("invalid fleet response")}
 return json.RawMessage(data),nil
}
