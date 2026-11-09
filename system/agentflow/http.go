package agentflow

import (
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
)

func sendJSON(w http.ResponseWriter,code int,value any){
 w.Header().Set("Content-Type","application/json")
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.WriteHeader(code)
 _=json.NewEncoder(w).Encode(value)
}
func readJSON(r *http.Request,dest any)error{
 r.Body=http.MaxBytesReader(nil,r.Body,512<<10)
 decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
 if err:=decoder.Decode(dest);err!=nil{return err}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return errors.New("unexpected request data")}
 return nil
}
func NewHTTPHandler(service Manager)http.Handler{
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){sendJSON(w,200,map[string]string{"status":"ok"})})
 mux.HandleFunc("GET /runs",func(w http.ResponseWriter,_ *http.Request){
  runs,err:=service.List();if err!=nil{sendJSON(w,500,map[string]string{"error":"runs unavailable"});return}
  sendJSON(w,200,runs)
 })
 mux.HandleFunc("POST /runs",func(w http.ResponseWriter,r *http.Request){
  var p Program
  if err:=readJSON(r,&p);err!=nil{sendJSON(w,400,map[string]string{"error":"invalid engineering request"});return}
  run,err:=service.Submit(p)
  if err!=nil{sendJSON(w,400,map[string]string{"error":err.Error()});return}
  sendJSON(w,201,run)
 })
 mux.HandleFunc("GET /runs/{id}",func(w http.ResponseWriter,r *http.Request){
  run,err:=service.Get(r.PathValue("id"))
  if err!=nil{sendJSON(w,404,map[string]string{"error":"engineering run unavailable"});return}
  sendJSON(w,200,run)
 })
 mux.HandleFunc("POST /runs/{id}/cancel",func(w http.ResponseWriter,r *http.Request){
  var input struct{Revision uint64 `json:"revision"`}
  if err:=readJSON(r,&input);err!=nil{sendJSON(w,400,map[string]string{"error":"invalid cancellation"});return}
  if err:=service.Cancel(r.PathValue("id"),input.Revision);err!=nil{sendJSON(w,409,map[string]string{"error":"run revision conflict"});return}
  run,err:=service.Get(r.PathValue("id"))
  if err!=nil{sendJSON(w,500,map[string]string{"error":"run unavailable"});return}
  sendJSON(w,200,run)
 })
 if teams,ok:=service.(TeamManager);ok{
  mux.HandleFunc("GET /teams",func(w http.ResponseWriter,_ *http.Request){
   data,err:=teams.ListTeams();if err!=nil{sendJSON(w,500,map[string]string{"error":"teams unavailable"});return}
   sendJSON(w,200,data)
  })
  mux.HandleFunc("POST /teams",func(w http.ResponseWriter,r *http.Request){
   var request TeamRequest
   if err:=readJSON(r,&request);err!=nil{sendJSON(w,400,map[string]string{"error":"invalid team request"});return}
   summary,err:=teams.SubmitTeam(request);if err!=nil{sendJSON(w,400,map[string]string{"error":err.Error()});return}
   sendJSON(w,201,summary)
  })
  mux.HandleFunc("GET /teams/{id}",func(w http.ResponseWriter,r *http.Request){
   summary,err:=teams.GetTeam(r.PathValue("id"));if err!=nil{sendJSON(w,404,map[string]string{"error":"team not found"});return}
   sendJSON(w,200,summary)
  })
  mux.HandleFunc("POST /teams/{id}/integrate",func(w http.ResponseWriter,r *http.Request){
   var request TeamIntegrationRequest
   if err:=readJSON(r,&request);err!=nil{sendJSON(w,400,map[string]string{"error":"invalid integration request"});return}
   team,err:=teams.IntegrateTeam(r.PathValue("id"),request)
   if err!=nil{sendJSON(w,409,map[string]string{"error":err.Error()});return}
   sendJSON(w,202,team)
  })
  mux.HandleFunc("POST /teams/{id}/cancel",func(w http.ResponseWriter,r *http.Request){
   if err:=teams.CancelTeam(r.PathValue("id"));err!=nil{sendJSON(w,409,map[string]string{"error":"team cannot be cancelled"});return}
   summary,err:=teams.GetTeam(r.PathValue("id"));if err!=nil{sendJSON(w,500,map[string]string{"error":"team unavailable"});return}
   sendJSON(w,200,summary)
  })
 }
 return mux
}
// ServeUnix exposes only lifecycle operations on a group-restricted local
// socket. The caller's gateway token is not a compute authorization token.
func ServeUnix(ctx context.Context,path string,service Manager)error{
 if service==nil||!filepath.IsAbs(path)||filepath.Ext(path)!=".sock"||strings.ContainsAny(path,"\x00\r\n"){
  return errors.New("invalid engineering socket configuration")
 }
 dir:=filepath.Dir(path)
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0007!=0{return errors.New("unsafe engineering socket directory")}
 if prior,err:=os.Lstat(path);err==nil{
  if prior.Mode()&os.ModeSocket==0{return errors.New("existing engineering socket path is unsafe")}
  if err:=os.Remove(path);err!=nil{return err}
 }else if !errors.Is(err,os.ErrNotExist){return err}
 listener,err:=net.Listen("unix",path);if err!=nil{return err}
 defer listener.Close()
 defer os.Remove(path)
 if err:=os.Chmod(path,0660);err!=nil{return err}
 server:=&http.Server{Handler:NewHTTPHandler(service),ReadHeaderTimeout:5*time.Second,ReadTimeout:15*time.Second,WriteTimeout:30*time.Second,IdleTimeout:20*time.Second}
 go func(){<-ctx.Done();shutdown,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel();_ = server.Shutdown(shutdown)}()
 err=server.Serve(listener)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
