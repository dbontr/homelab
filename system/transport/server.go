package transport

import (
 "context"
 "crypto/subtle"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "os"
 "strconv"
 "strings"
 "time"
 "github.com/dbontr/homelab-new/system/approval"
 "github.com/dbontr/homelab-new/system/calendar"
 "github.com/dbontr/homelab-new/system/devices"
 "github.com/dbontr/homelab-new/system/media"
 "github.com/dbontr/homelab-new/system/profiles"
 "github.com/dbontr/homelab-new/system/store"
)
type Backend struct { Name string; URL string; Token string }
type Server struct { db *store.Store; token string; backend []Backend; client *http.Client; calendarURL string; gate *approval.Gate; allowedOrigins map[string]struct{}; media *media.Client; deviceCatalog *devices.Catalog; nodeCatalog *devices.NodeCatalog; profiles *profiles.Registry }

func New(db *store.Store,token string,backends []Backend)(*Server,error){
 if db==nil||token==""{return nil,errors.New("store and access token are required")}
 used:=map[string]bool{}
 for _,b:=range backends{
  if b.Name==""||used[b.Name]||strings.Contains(b.Name,"__"){return nil,errors.New("invalid backend name")}
  used[b.Name]=true
  u,e:=url.Parse(b.URL);if e!=nil{return nil,e}
  if u.User!=nil||u.RawQuery!=""||u.Fragment!=""{return nil,errors.New("invalid backend URL")}
  if u.Scheme!="https"&&!(u.Scheme=="http"&&(u.Hostname()=="localhost"||u.Hostname()=="127.0.0.1"||u.Hostname()=="::1")){return nil,errors.New("backend must use HTTPS or loopback HTTP")}
 }
 return &Server{db:db,token:token,backend:backends,gate:approval.New(),client:&http.Client{Timeout:120*time.Second,CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return errors.New("backend redirects are disabled")}}},nil
}
func(s *Server) SetDeviceCatalog(catalog *devices.Catalog) { s.deviceCatalog = catalog }
func(s *Server) SetNodeCatalog(catalog *devices.NodeCatalog) { s.nodeCatalog = catalog }
func(s *Server) SetProfiles(registry *profiles.Registry) { s.profiles = registry }
func(s *Server) SetCalendarURL(raw string)error{
 if err:=calendar.ValidateURL(raw);err!=nil{return err}
 s.calendarURL=raw
 return nil
}
func(s *Server) Handler()http.Handler{
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,map[string]string{"status":"ok"})})
 mux.HandleFunc("POST /mcp",s.mcp)
 mux.HandleFunc("GET /v1/software",s.softwareLibrary)
 mux.HandleFunc("GET /v1/tools",func(w http.ResponseWriter,r *http.Request) {
  writeJSON(w,200,map[string]any{"tools":s.toolList(r)})
 })
 mux.HandleFunc("POST /v1/tools/{name}/call",s.nativeToolCall)
 mux.HandleFunc("GET /v1/tasks",s.tasks)
 mux.HandleFunc("POST /v1/tasks",s.tasks)
 mux.HandleFunc("POST /v1/tasks/{id}/complete",s.complete)
 mux.HandleFunc("GET /v1/goals",s.goals)
 mux.HandleFunc("POST /v1/goals",s.goals)
 mux.HandleFunc("GET /v1/rules",s.rules)
 mux.HandleFunc("PUT /v1/rules/{key}",s.rules)
 mux.HandleFunc("GET /v1/recommendations",s.recommendations)
 mux.HandleFunc("GET /v1/reports/weekly",s.weekly)
 mux.HandleFunc("GET /v1/calendar",s.calendarEvents)
 mux.HandleFunc("GET /v1/devices",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,s.deviceCatalog.List())})
 mux.HandleFunc("GET /v1/nodes",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,s.nodeCatalog.List())})
 mux.HandleFunc("GET /v1/compute/profiles",s.profileRoutes)
 mux.HandleFunc("GET /v1/compute/profiles/{id}",s.profileRoutes)
 mux.HandleFunc("PUT /v1/compute/profiles/{id}",s.profileRoutes)
 mux.HandleFunc("POST /v1/compute/profiles/{id}/plan",s.profilePlanRoute)
 mux.HandleFunc("GET /v1/media/search",s.mediaSearch)
 mux.HandleFunc("GET /v1/media/requests",s.mediaRequests)
 mux.HandleFunc("POST /v1/media/requests",s.mediaRequests)
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  if r.URL.Path!="/healthz" {
   if !s.originAllowed(r) {writeError(w,http.StatusForbidden,"origin not permitted");return}
   auth:=r.Header.Get("Authorization")
   if !strings.HasPrefix(auth,"Bearer ")||subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth,"Bearer ")),[]byte(s.token))!=1{
    writeError(w,401,"authorization required");return
   }
  }
  mux.ServeHTTP(w,r)
 })
}
func writeJSON(w http.ResponseWriter,code int,v any){w.Header().Set("Content-Type","application/json");w.WriteHeader(code);_ = json.NewEncoder(w).Encode(v)}
func writeError(w http.ResponseWriter,code int,msg string){writeJSON(w,code,map[string]string{"error":msg})}
func decode(r *http.Request,v any)error{
 r.Body=http.MaxBytesReader(nil,r.Body,1<<20)
 d:=json.NewDecoder(r.Body);d.DisallowUnknownFields()
 if e:=d.Decode(v);e!=nil{return e}
 var extra any
 if e:=d.Decode(&extra);e!=io.EOF{return errors.New("request must contain one JSON object")}
 return nil
}
func(s *Server)tasks(w http.ResponseWriter,r *http.Request){
 if r.Method=="GET"{writeJSON(w,200,s.db.Tasks(r.URL.Query().Get("status")));return}
 var in store.TaskInput
 if e:=decode(r,&in);e!=nil{writeError(w,400,e.Error());return}
 t,e:=s.db.AddTask(in);if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,201,t)
}
func(s *Server)complete(w http.ResponseWriter,r *http.Request){
 t,e:=s.db.CompleteTask(r.PathValue("id"))
 if errors.Is(e,os.ErrNotExist){writeError(w,404,"task not found");return}
 if e!=nil{writeError(w,500,"task completion failed");return}
 writeJSON(w,200,t)
}
func(s *Server)goals(w http.ResponseWriter,r *http.Request){
 if r.Method=="GET"{writeJSON(w,200,s.db.Goals());return}
 var in struct {Title string `json:"title"`; Notes string `json:"notes"`}
 if e:=decode(r,&in);e!=nil{writeError(w,400,e.Error());return}
 g,e:=s.db.AddGoal(in.Title,in.Notes);if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,201,g)
}
func(s *Server)rules(w http.ResponseWriter,r *http.Request){
 if r.Method=="GET"{writeJSON(w,200,s.db.Rules());return}
 var in struct {Text string `json:"text"`; Kind string `json:"kind"`}
 if e:=decode(r,&in);e!=nil{writeError(w,400,e.Error());return}
 v,e:=s.db.SetRule(r.PathValue("key"),in.Text,in.Kind);if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,200,v)
}
func(s *Server)recommendations(w http.ResponseWriter,r *http.Request){
 q:=r.URL.Query();minutes,_:=strconv.Atoi(q.Get("minutes"));limit,_:=strconv.Atoi(q.Get("limit"))
 writeJSON(w,200,s.db.Suggest(minutes,q.Get("energy"),q.Get("location"),q.Get("mode"),limit))
}
func(s *Server)weekly(w http.ResponseWriter,r *http.Request){
 v,e:=s.db.Report(r.URL.Query().Get("week_of"));if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,200,v)
}
func(s *Server)calendarEvents(w http.ResponseWriter,r *http.Request){
 if s.calendarURL==""{writeError(w,503,"calendar is not configured");return}
 feed,e:=calendar.Fetch(r.Context(),s.client,s.calendarURL)
 if e!=nil{writeError(w,502,"calendar feed unavailable");return}
 writeJSON(w,200,feed)
}
type rpcRequest struct { JSONRPC string `json:"jsonrpc"`; ID json.RawMessage `json:"id,omitempty"`; Method string `json:"method"`; Params json.RawMessage `json:"params,omitempty"` }
type rpcResponse struct { JSONRPC string `json:"jsonrpc"`; ID json.RawMessage `json:"id,omitempty"`; Result json.RawMessage `json:"result,omitempty"`; Error *rpcError `json:"error,omitempty"` }
type rpcError struct { Code int `json:"code"`; Message string `json:"message"` }
func(s *Server)rpc(w http.ResponseWriter,id json.RawMessage,v any,e *rpcError){
 out:=map[string]any{"jsonrpc":"2.0","id":id}
 if e!=nil{out["error"]=e}else{out["result"]=v}
 writeJSON(w,200,out)
}
func failure(code int,msg string)*rpcError{return &rpcError{Code:code,Message:msg}}
func result(v any)map[string]any{
 b,e:=json.Marshal(v)
 if e!=nil{return map[string]any{"content":[]any{map[string]string{"type":"text","text":e.Error()}},"isError":true}}
 return map[string]any{"content":[]any{map[string]string{"type":"text","text":string(b)}},"isError":false}
}
func toolError(msg string)map[string]any{return map[string]any{"content":[]any{map[string]string{"type":"text","text":msg}},"isError":true}}
func(s *Server)mcp(w http.ResponseWriter,r *http.Request){
 r.Body=http.MaxBytesReader(w,r.Body,8<<20)
 var req rpcRequest
 if e:=json.NewDecoder(r.Body).Decode(&req);e!=nil{s.rpc(w,nil,nil,failure(-32700,"invalid JSON"));return}
 if req.JSONRPC!="2.0"||req.Method==""{s.rpc(w,req.ID,nil,failure(-32600,"invalid JSON-RPC request"));return}
 if len(req.ID)==0{
  if strings.HasPrefix(req.Method,"notifications/"){w.WriteHeader(http.StatusAccepted);return}
  s.rpc(w,nil,nil,failure(-32600,"request ID required"));return
 }
 switch req.Method{
 case "initialize":
  s.rpc(w,req.ID,map[string]any{"protocolVersion":"2025-06-18","capabilities":map[string]any{"tools":map[string]any{"listChanged":false}},"serverInfo":map[string]string{"name":"homelab","version":"0.1.0"}},nil)
 case "ping":s.rpc(w,req.ID,map[string]any{},nil)
 case "tools/list":s.rpc(w,req.ID,map[string]any{"tools":s.toolList(r)},nil)
 case "tools/call":
  var p struct {Name string `json:"name"`; Arguments json.RawMessage `json:"arguments"`}
  if e:=json.Unmarshal(req.Params,&p);e!=nil||p.Name==""{s.rpc(w,req.ID,nil,failure(-32602,"tool name required"));return}
  s.rpc(w,req.ID,s.invoke(r.Context(),p.Name,p.Arguments,req.ID),nil)
 default:s.rpc(w,req.ID,nil,failure(-32601,"method not found"))
 }
}
func tool(name,description string,properties map[string]any,required ...string)map[string]any{
 if required==nil{required=[]string{}}
 return map[string]any{"name":name,"description":description,"inputSchema":map[string]any{"type":"object","properties":properties,"required":required}}
}
func prop(kind string)map[string]any{return map[string]any{"type":kind}}
func(s *Server)localTools()[]map[string]any{
 tools:=[]map[string]any{
  tool("task_add","Create a personal task",map[string]any{"parent_id":prop("string"),"depends_on":map[string]any{"type":"array","items":prop("string")},"title":prop("string"),"notes":prop("string"),"goal_id":prop("string"),"due":prop("string"),"minutes":prop("integer"),"energy":prop("string"),"location":prop("string"),"priority":prop("integer")},"title"),
  tool("task_complete","Mark a task complete",map[string]any{"id":prop("string")},"id"),
  tool("task_list","List personal tasks",map[string]any{"status":prop("string")}),
  tool("task_suggest","Recommend tasks by time, mental effort, location, or quick-win mode",map[string]any{"minutes":prop("integer"),"energy":prop("string"),"location":prop("string"),"mode":prop("string"),"limit":prop("integer")}),
  tool("goal_add","Add a personal goal",map[string]any{"title":prop("string"),"notes":prop("string")},"title"),
  tool("goal_list","List personal goals",map[string]any{}),
  tool("rule_set","Store a personal constraint or preference; only structured rules are mechanically enforced",map[string]any{"key":prop("string"),"text":prop("string"),"kind":prop("string")},"key","text","kind"),
  tool("rule_list","List personal rules",map[string]any{}),
  tool("weekly_report","Summarize recorded task completions",map[string]any{"week_of":prop("string")}),
  tool("system_status","List configured integrations",map[string]any{}),
  tool("approval_status","Check an administrator approval",map[string]any{"id":prop("string")},"id"),
  tool("device_inventory","List declared hardware access and constraints; this is not an online device probe",map[string]any{}),
  tool("node_inventory","List declarative management modes and roles without probing machines",map[string]any{}),
 }
 if s.hasBackend("workspace") {tools=append(tools,tool("software_catalog","List approved images, software collections, and program requests",map[string]any{}))}
 if s.profiles!=nil {tools=append(tools,
  tool("compute_profile_list","List saved reusable build configurations",map[string]any{}),
  tool("compute_profile_get","Read a saved build configuration revision",map[string]any{"id":prop("string"),"version":prop("integer")},"id"),
  tool("compute_profile_save","Save an immutable build profile revision",map[string]any{"spec":prop("object"),"expected_version":prop("integer")},"spec","expected_version"),
  tool("compute_profile_plan","Check available Workspace resources for a saved profile without scheduling",map[string]any{"id":prop("string"),"version":prop("integer"),"project_id":prop("string"),"node_id":prop("string")},"id","project_id"))}
 if s.calendarURL!=""{tools=append(tools,tool("calendar_feed","Read Proton Calendar event definitions; recurrence rules are not expanded",map[string]any{}))}
 if s.media!=nil{tools=append(tools,
  tool("media_search","Search movies and television through Seerr",map[string]any{"query":prop("string")},"query"),
  tool("media_requests","List Seerr media requests",map[string]any{}),
  tool("media_request","Request a movie or television season through Seerr",map[string]any{"mediaType":prop("string"),"mediaId":prop("integer"),"seasons":map[string]any{"type":"array","items":prop("integer")},"is4k":prop("boolean")},"mediaType","mediaId"))}
 return tools
}
func(s *Server)toolList(r *http.Request)[]map[string]any{
 out:=s.localTools()
 for _,b:=range s.backend{
  var remote any
  ctx,cancel:=context.WithTimeout(r.Context(),4*time.Second)
  e:=s.forward(ctx,b,rpcRequest{JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/list"},&remote)
  cancel()
  if e!=nil{continue}
  response,ok:=remote.(map[string]any);if !ok{continue}
  list,ok:=response["tools"].([]any);if !ok{continue}
  for _,item:=range list{
   t,ok:=item.(map[string]any);if !ok{continue}
   name,ok:=t["name"].(string);if !ok||name==""{continue}
   t["name"]=b.Name+"__"+name
   out=append(out,t)
  }
 }
 return out
}
func(s *Server)localTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
 if s.media!=nil&&(name=="media_search"||name=="media_request"||name=="media_requests"){return s.mediaTool(ctx,name,raw)}
 if name=="compute_profile_list"||name=="compute_profile_get"||name=="compute_profile_save"||name=="compute_profile_plan" {return s.profileTool(ctx,name,raw)}
 var args struct{
  store.TaskInput
  ID string `json:"id"`
  Status string `json:"status"`
  Mode string `json:"mode"`
  Limit int `json:"limit"`
  Key string `json:"key"`
  Text string `json:"text"`
  Kind string `json:"kind"`
  WeekOf string `json:"week_of"`
 }
 if len(raw)>0{if e:=json.Unmarshal(raw,&args);e!=nil{return nil,e}}
 switch name{
 case "task_add":return s.db.AddTask(args.TaskInput)
 case "task_complete":return s.db.CompleteTask(args.ID)
 case "task_list":return s.db.Tasks(args.Status),nil
 case "task_suggest":return s.db.Suggest(args.Minutes,args.Energy,args.Location,args.Mode,args.Limit),nil
 case "goal_add":return s.db.AddGoal(args.Title,args.Notes)
 case "goal_list":return s.db.Goals(),nil
 case "rule_set":return s.db.SetRule(args.Key,args.Text,args.Kind)
 case "rule_list":return s.db.Rules(),nil
 case "weekly_report":return s.db.Report(args.WeekOf)
 case "approval_status":return s.privilegeStatus(args.ID)
 case "device_inventory":return s.deviceCatalog.List(),nil
 case "node_inventory":return s.nodeCatalog.List(),nil
 case "calendar_feed":
  if s.calendarURL==""{return nil,errors.New("calendar is not configured")}
  return calendar.Fetch(ctx,s.client,s.calendarURL)
 case "software_catalog":return s.softwareEntries(ctx)
 case "system_status":
  statuses:=map[string]string{}
  for _,b:=range s.backend{
   checkCtx,cancel:=context.WithTimeout(ctx,4*time.Second)
   var output any
   e:=s.forward(checkCtx,b,rpcRequest{JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/list"},&output)
   cancel()
   if e!=nil{statuses[b.Name]="unavailable"}else{statuses[b.Name]="available"}
  }
  return map[string]any{"integrations":statuses,"local":"available"},nil
 default:return nil,fmt.Errorf("unknown tool: %s",name)
 }
}
