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
 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/approval"
 "github.com/dbontr/homelab-new/system/calendar"
 "github.com/dbontr/homelab-new/system/books"
 "github.com/dbontr/homelab-new/system/devices"
 "github.com/dbontr/homelab-new/system/media"
 "github.com/dbontr/homelab-new/system/observability"
 "github.com/dbontr/homelab-new/system/profiles"
 "github.com/dbontr/homelab-new/system/recipes"
 "github.com/dbontr/homelab-new/system/session"
 "github.com/dbontr/homelab-new/system/security"
 "github.com/dbontr/homelab-new/system/privileged"
 "github.com/dbontr/homelab-new/system/release"
 "github.com/dbontr/homelab-new/system/store"
)
type Backend struct { Name string; URL string; Token string }
type Server struct { db *store.Store; token string; backend []Backend; client *http.Client; calendarURL string; gate *approval.Gate; privilegeClient *security.AdminClient; executorClient *privileged.Client; fleetClient *release.FleetClient; securityLoginURL string; allowedOrigins map[string]struct{}; media *media.Client; deviceCatalog *devices.Catalog; nodeCatalog *devices.NodeCatalog; profiles *profiles.Registry; recipes *recipes.Store; sessions *session.Manager; observability *observability.Client; bookCatalog *books.Catalog; engineering agentflow.Manager; workflows *agentflow.Store; capabilityPlanner agentflow.CapabilityPlanner; inflight chan struct{} }

func New(db *store.Store,token string,backends []Backend)(*Server,error){
 if db==nil||len(token)<32||strings.ContainsAny(token,"\r\n\t"){return nil,errors.New("store and a strong access token are required")}
 used:=map[string]bool{}
 for _,b:=range backends{
  if !validIntegrationName(b.Name)||used[b.Name]{return nil,errors.New("invalid backend name")}
  used[b.Name]=true
  u,e:=url.Parse(b.URL);if e!=nil{return nil,errors.New("invalid backend URL")}
  if u.Hostname()==""||u.Opaque!=""{return nil,errors.New("backend URL requires a host")}
  if u.User!=nil||u.RawQuery!=""||u.Fragment!=""{return nil,errors.New("invalid backend URL")}
  if u.Scheme!="https"&&!(u.Scheme=="http"&&(u.Hostname()=="localhost"||u.Hostname()=="127.0.0.1"||u.Hostname()=="::1")){return nil,errors.New("backend must use HTTPS or loopback HTTP")}
 }
 return &Server{db:db,token:token,backend:backends,gate:approval.New(),inflight:make(chan struct{},32),client:&http.Client{Timeout:120*time.Second,CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return errors.New("backend redirects are disabled")}}},nil
}
func(s *Server) SetDeviceCatalog(catalog *devices.Catalog) { s.deviceCatalog = catalog }
func(s *Server) SetNodeCatalog(catalog *devices.NodeCatalog) { s.nodeCatalog = catalog }
func(s *Server) SetProfiles(registry *profiles.Registry) { s.profiles = registry }
func(s *Server) SetBookCatalog(catalog *books.Catalog) { s.bookCatalog = catalog }
func(s *Server) SetSessions(manager *session.Manager) { s.sessions = manager }
func(s *Server) SetPrivilegeClient(client *security.AdminClient) { s.privilegeClient = client }
func(s *Server) SetExecutorClient(client *privileged.Client) {s.executorClient=client}
func(s *Server) SetFleetClient(client *release.FleetClient) {s.fleetClient=client}
func(s *Server) SetSecurityLoginURL(raw string) error {
 u,err:=url.Parse(raw)
 if err!=nil || u.Scheme!="https" || u.Host=="" || u.User!=nil || u.RawQuery!="" || u.Fragment!="" || (u.Path!=""&&u.Path!="/") {
  return errors.New("security login must use an exact HTTPS origin")
 }
 if u.Hostname()=="localhost" || u.Hostname()=="127.0.0.1" || u.Hostname()=="::1" {
  return errors.New("security login must use a protected external hostname")
 }
 s.securityLoginURL="https://"+u.Host
 return nil
}
func(s *Server) SetCalendarURL(raw string)error{
 if err:=calendar.ValidateURL(raw);err!=nil{return err}
 s.calendarURL=raw
 return nil
}
func(s *Server) Handler()http.Handler{
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,map[string]string{"status":"ok"})})
 mux.HandleFunc("GET /ui/session",s.uiSession)
 mux.HandleFunc("POST /ui/session",s.uiSession)
 mux.HandleFunc("DELETE /ui/session",s.uiSession)
 mux.HandleFunc("GET /v1/engineering/projects",s.uiProjects)
 mux.HandleFunc("GET /v1/engineering/projects/{project}/jobs",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/engineering/projects/{project}/workspaces",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/engineering/projects/{project}/context",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/engineering/projects/{project}/repositories",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/engineering/projects/{project}/research-sources",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/engineering/projects/{project}/research",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/engineering/nodes",s.uiComputeNodes)
 mux.HandleFunc("GET /v1/engineering/runs",s.engineeringCollection)
 mux.HandleFunc("POST /v1/engineering/runs",s.engineeringCollection)
 mux.HandleFunc("GET /v1/engineering/runs/{id}",s.engineeringRun)
 mux.HandleFunc("POST /v1/engineering/runs/{id}/cancel",s.engineeringRun)
 mux.HandleFunc("GET /v1/engineering/teams",s.engineeringTeamCollection)
 mux.HandleFunc("POST /v1/engineering/teams",s.engineeringTeamCollection)
 mux.HandleFunc("GET /v1/engineering/teams/{id}",s.engineeringTeam)
 mux.HandleFunc("POST /v1/engineering/teams/{id}/cancel",s.engineeringTeam)
 mux.HandleFunc("POST /v1/engineering/teams/{id}/integrate",s.engineeringTeamIntegrate)
 mux.HandleFunc("GET /v1/engineering/runs/{id}/knowledge/{proposal}",s.engineeringRunKnowledge)
 mux.HandleFunc("POST /v1/engineering/runs/{id}/knowledge/{proposal}/approve",s.engineeringRunKnowledge)
 mux.HandleFunc("GET /v1/engineering/runs/{id}/diffs",s.engineeringRunDiffs)
 mux.HandleFunc("GET /v1/projects/{project}/overview",s.uiProjectOverview)
 mux.HandleFunc("POST /v1/workflows/plan",s.capabilityWorkflowPlan)
 mux.HandleFunc("GET /v1/automations",s.automationCollection)
 mux.HandleFunc("POST /v1/automations",s.automationCollection)
 mux.HandleFunc("GET /v1/automations/alerts",s.automationAlerts)
 mux.HandleFunc("POST /v1/automations/{id}/alerts/{run}/ack",s.automationAlertAck)
 mux.HandleFunc("GET /v1/automations/{id}",s.automationItem)
 mux.HandleFunc("POST /v1/automations/{id}/enabled",s.automationItem)
 mux.HandleFunc("POST /v1/research/learning/preview",s.researchLearningHTTP)
 mux.HandleFunc("POST /v1/research/learning/import",s.researchLearningHTTP)
 mux.HandleFunc("GET /v1/workflows",s.capabilityWorkflowCollection)
 mux.HandleFunc("POST /v1/workflows",s.capabilityWorkflowCollection)
 mux.HandleFunc("GET /v1/workflows/{id}",s.capabilityWorkflowItem)
 mux.HandleFunc("POST /v1/workflows/{id}/cancel",s.capabilityWorkflowItem)
 mux.HandleFunc("GET /v1/workspace/projects",s.uiProjects)
 mux.HandleFunc("GET /v1/workspace/projects/{project}/jobs",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/workspace/projects/{project}/workspaces",s.uiWorkspaceProjectView)
 mux.HandleFunc("GET /v1/learning/summary",s.uiLearningSummary)
 mux.HandleFunc("GET /v1/learning/sessions",s.uiLearningSessions)
 mux.HandleFunc("POST /v1/learning/sessions",s.uiLearningStart)
 mux.HandleFunc("GET /v1/learning/sessions/{id}",s.uiLearningSession)
 mux.HandleFunc("GET /v1/learning/sessions/{id}/pending",s.uiLearningPending)
 mux.HandleFunc("POST /v1/learning/questions/{id}/answer",s.uiLearningAnswer)
 mux.HandleFunc("GET /v1/learning/reviews",s.uiLearningReviews)
 mux.HandleFunc("GET /v1/health/plans",s.uiHealthPlans)
 mux.HandleFunc("GET /v1/health/records",s.uiHealthRecords)
 mux.HandleFunc("GET /v1/health/training-sets",s.uiHealthTrainingList)
 mux.HandleFunc("POST /v1/health/training-sets",s.uiHealthTrainingLog)
 mux.HandleFunc("GET /v1/health/training-progress",s.uiHealthTrainingProgress)
 mux.HandleFunc("POST /v1/health/records",s.uiHealthObservation)
 mux.HandleFunc("GET /v1/health/goals",s.uiHealthGoals)
 mux.HandleFunc("GET /v1/health/food-proposals",s.uiFoodProposals)
 mux.HandleFunc("POST /v1/health/food-proposals/{id}/confirm",s.uiFoodConfirm)
 mux.HandleFunc("POST /v1/recipes/{id}/nutrition-proposal",s.uiRecipeNutritionProposal)
 mux.HandleFunc("GET /v1/pantry",s.pantryCollection)
 mux.HandleFunc("PUT /v1/pantry",s.pantryCollection)
 mux.HandleFunc("DELETE /v1/pantry/items/{key}",s.pantryItem)
 mux.HandleFunc("POST /v1/recipes/recommend",s.pantryRecommendations)
 mux.HandleFunc("GET /v1/recipes",s.recipeCollection)
 mux.HandleFunc("POST /v1/recipes",s.recipeCollection)
 mux.HandleFunc("GET /v1/recipes/health-goals",s.uiHealthGoals)
 mux.HandleFunc("GET /v1/recipes/export",s.recipeArchive)
 mux.HandleFunc("GET /v1/recipes/bundle",s.recipeBundle)
 mux.HandleFunc("POST /v1/recipes/bundle",s.recipeBundle)
 mux.HandleFunc("POST /v1/recipes/parse",s.recipeParse)
 mux.HandleFunc("POST /v1/recipes/import-url",s.recipeImportURL)
 mux.HandleFunc("POST /v1/recipes/recipesage/preview",s.recipeSagePreview)
 mux.HandleFunc("POST /v1/recipes/recipesage/import",s.recipeSageImport)
 mux.HandleFunc("POST /v1/recipes/images",s.recipeImageUpload)
 mux.HandleFunc("GET /v1/recipes/images/{name}",s.recipeImage)
 mux.HandleFunc("POST /v1/recipes/import",s.recipeArchive)
 mux.HandleFunc("GET /v1/recipes/{id}",s.recipeItem)
 mux.HandleFunc("PUT /v1/recipes/{id}",s.recipeItem)
 mux.HandleFunc("DELETE /v1/recipes/{id}",s.recipeItem)
 mux.HandleFunc("POST /v1/recipes/{id}/usda-preview",s.recipeUSDAHTTP)
 mux.HandleFunc("POST /v1/recipes/{id}/usda-apply",s.recipeUSDAHTTP)
 mux.HandleFunc("GET /v1/recipes/{id}/nutrition",s.recipeNutrition)
 mux.HandleFunc("GET /v1/recipes/{id}/history",s.recipeHistory)
 mux.HandleFunc("POST /v1/recipes/{id}/restore",s.recipeRestore)
 mux.HandleFunc("GET /v1/meals",s.mealCollection)
 mux.HandleFunc("POST /v1/meals",s.mealCollection)
 mux.HandleFunc("DELETE /v1/meals/{id}",s.mealItem)
 mux.HandleFunc("GET /v1/groceries",s.recipeGroceries)
 mux.HandleFunc("GET /v1/groceries/needs",s.pantryGroceryNeeds)
 mux.HandleFunc("GET /v1/groceries/board",s.groceryBoard)
 mux.HandleFunc("PUT /v1/groceries/items/{key}",s.groceryUpdate)
 mux.HandleFunc("GET /v1/activity",s.uiActivity)
 mux.HandleFunc("GET /v1/resources/{kind}/{id}",s.uiResource)
 mux.HandleFunc("GET /v1/resources/resolve",s.uiResourceReference)
 mux.HandleFunc("GET /v1/workspace/nodes",s.uiComputeNodes)
 for _,path:=range []string{"/v1/monitoring/summary","/v1/monitoring/targets",
   "/v1/monitoring/alerts","/v1/monitoring/security","/v1/monitoring/series","/v1/monitoring/logs"} {
  mux.HandleFunc("GET "+path,s.uiMonitoring)
 }
 mux.HandleFunc("POST /mcp",s.mcp)
 mux.HandleFunc("GET /v1/software",s.softwareLibrary)
 mux.HandleFunc("GET /v1/tools",func(w http.ResponseWriter,r *http.Request) {
  writeJSON(w,200,map[string]any{"tools":s.toolList(r)})
 })
 mux.HandleFunc("POST /v1/tools/{name}/call",s.nativeToolCall)
 mux.HandleFunc("GET /v1/tasks/delegations",s.uiTaskDelegations)
 mux.HandleFunc("GET /v1/tasks",s.tasks)
 mux.HandleFunc("POST /v1/tasks",s.tasks)
 mux.HandleFunc("POST /v1/tasks/{id}/complete",s.complete)
 mux.HandleFunc("POST /v1/tasks/{id}/decompose",s.taskDecompose)
 mux.HandleFunc("POST /v1/tasks/capture",s.taskCapture)
 mux.HandleFunc("GET /v1/tasks/agenda",s.taskAgenda)
 mux.HandleFunc("GET /v1/tasks/conflicts",s.taskConflicts)
 mux.HandleFunc("PUT /v1/tasks/{id}",s.taskUpdate)
 mux.HandleFunc("GET /v1/goals",s.goals)
 mux.HandleFunc("POST /v1/goals",s.goals)
 mux.HandleFunc("GET /v1/rules",s.rules)
 mux.HandleFunc("PUT /v1/rules/{key}",s.rules)
 mux.HandleFunc("GET /v1/recommendations",s.recommendations)
 mux.HandleFunc("GET /v1/reports/weekly",s.weekly)
 mux.HandleFunc("GET /v1/planning/week",s.uiWeekPlan)
 mux.HandleFunc("GET /v1/planning/review",s.uiTaskReview)
 mux.HandleFunc("GET /v1/calendar",s.calendarEvents)
 mux.HandleFunc("GET /v1/calendar/occurrences",s.calendarOccurrences)
 mux.HandleFunc("GET /v1/devices",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,s.deviceCatalog.List())})
 mux.HandleFunc("GET /v1/nodes",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,200,s.nodeCatalog.List())})
 mux.HandleFunc("GET /v1/compute/profiles",s.profileRoutes)
 mux.HandleFunc("GET /v1/compute/profiles/{id}",s.profileRoutes)
 mux.HandleFunc("PUT /v1/compute/profiles/{id}",s.profileRoutes)
 mux.HandleFunc("POST /v1/compute/profiles/{id}/plan",s.profilePlanRoute)
 mux.HandleFunc("GET /v1/books/search",s.bookRoutes)
 mux.HandleFunc("GET /v1/books/lending",s.bookRoutes)
 mux.HandleFunc("GET /v1/books/requests",s.bookRoutes)
 mux.HandleFunc("POST /v1/books/requests",s.bookRoutes)
 mux.HandleFunc("PUT /v1/books/requests/{id}",s.bookRoutes)
 mux.HandleFunc("GET /v1/media/search",s.mediaSearch)
 mux.HandleFunc("GET /v1/media/requests",s.mediaRequests)
 mux.HandleFunc("POST /v1/media/requests",s.mediaRequests)
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("Referrer-Policy","no-referrer")
  select {case s.inflight<-struct{}{}: defer func(){<-s.inflight}()
   default: w.Header().Set("Retry-After","1");writeError(w,http.StatusTooManyRequests,"request capacity exhausted");return
  }
  if r.URL.Path!="/healthz" {
   if !s.originAllowed(r) {writeError(w,http.StatusForbidden,"origin not permitted");return}
   if r.URL.Path=="/ui/session" {mux.ServeHTTP(w,r);return}
   auth:=r.Header.Get("Authorization")
   bearer:=strings.HasPrefix(auth,"Bearer ") &&
     subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth,"Bearer ")),[]byte(s.token))==1
   if !bearer && !s.sessionReadAllowed(r) && !s.sessionOwnerWriteAllowed(r) {
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
 decoder:=json.NewDecoder(r.Body)
 if e:=decoder.Decode(&req);e!=nil{s.rpc(w,nil,nil,failure(-32700,"invalid JSON"));return}
 var trailing any
 if e:=decoder.Decode(&trailing);e!=io.EOF{s.rpc(w,nil,nil,failure(-32700,"multiple JSON messages are not permitted"));return}
 if req.JSONRPC!="2.0"||req.Method==""{s.rpc(w,req.ID,nil,failure(-32600,"invalid JSON-RPC request"));return}
 if len(req.ID)==0{
  if strings.HasPrefix(req.Method,"notifications/"){w.WriteHeader(http.StatusAccepted);return}
  s.rpc(w,nil,nil,failure(-32600,"request ID required"));return
 }
 switch req.Method{
 case "initialize":
  caps:=map[string]any{"tools":map[string]any{"listChanged":false}}
  if s.privilegeClient!=nil&&s.securityLoginURL!="" {caps["resources"]=map[string]any{"listChanged":false}}
  s.rpc(w,req.ID,map[string]any{"protocolVersion":"2025-06-18","capabilities":caps,"serverInfo":map[string]string{"name":"homelab","version":"0.1.0"}},nil)
 case "ping":s.rpc(w,req.ID,map[string]any{},nil)
 case "tools/list":s.rpc(w,req.ID,map[string]any{"tools":s.toolList(r)},nil)
 case "resources/list":
  if s.privilegeClient==nil||s.securityLoginURL==""{s.rpc(w,req.ID,map[string]any{"resources":[]any{}},nil);return}
  s.rpc(w,req.ID,map[string]any{"resources":[]any{map[string]any{"uri":"ui://homelab/security-login.html","name":"Homelab owner authentication","mimeType":"text/html;profile=mcp-app"}}},nil)
 case "resources/read":
  var p struct{URI string `json:"uri"`}
  if err:=json.Unmarshal(req.Params,&p);err!=nil||s.privilegeClient==nil||s.securityLoginURL==""||p.URI!="ui://homelab/security-login.html"{
   s.rpc(w,req.ID,nil,failure(-32602,"unknown UI resource"));return
  }
  s.rpc(w,req.ID,map[string]any{"contents":[]any{map[string]any{
   "uri":p.URI,"mimeType":"text/html;profile=mcp-app","text":s.securityLoginWidget(),
   "_meta":map[string]any{
    "ui":map[string]any{"csp":map[string]any{"connectDomains":[]string{},"resourceDomains":[]string{}}},
    "openai/widgetCSP":map[string]any{"redirect_domains":[]string{s.securityLoginURL}},
   },
  }}},nil)
 case "tools/call":
  var p struct {Name string `json:"name"`; Arguments json.RawMessage `json:"arguments"`}
  if e:=json.Unmarshal(req.Params,&p);e!=nil||p.Name==""{s.rpc(w,req.ID,nil,failure(-32602,"tool name required"));return}
  s.rpc(w,req.ID,s.invoke(security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion")),p.Name,p.Arguments,req.ID),nil)
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
  tool("task_add","Create a personal task",map[string]any{"links":map[string]any{"type":"array","items":prop("string")},"parent_id":prop("string"),"depends_on":map[string]any{"type":"array","items":prop("string")},"title":prop("string"),"notes":prop("string"),"goal_id":prop("string"),"due":prop("string"),"minutes":prop("integer"),"energy":prop("string"),"location":prop("string"),"priority":prop("integer")},"title"),
  tool("task_complete","Mark a task complete",map[string]any{"id":prop("string")},"id"),
  tool("task_decompose","Atomically split an open parent task into actionable subtasks, with prerequisites referencing earlier step indices",map[string]any{
   "parent_id":prop("string"),
   "expected_revision":prop("integer"),
   "steps":map[string]any{"type":"array","minItems":2,"maxItems":32,"items":map[string]any{
    "type":"object","properties":map[string]any{
     "title":prop("string"),"notes":prop("string"),"minutes":prop("integer"),
     "energy":prop("string"),"location":prop("string"),"priority":prop("integer"),
     "depends_on_steps":map[string]any{"type":"array","items":prop("integer")},
    },"required":[]string{"title","minutes"},
   }},
  },"parent_id","expected_revision","steps"),
  tool("task_list","List personal tasks",map[string]any{"status":prop("string")}),
  tool("task_delegations","Read evidence-linked engineering delegation status without completing personal tasks",map[string]any{}),
  tool("task_suggest","Recommend tasks by time, mental effort, location, or quick-win mode",map[string]any{"minutes":prop("integer"),"energy":prop("string"),"location":prop("string"),"mode":prop("string"),"limit":prop("integer")}),
  tool("goal_add","Add a personal goal",map[string]any{"title":prop("string"),"notes":prop("string")},"title"),
  tool("goal_list","List personal goals",map[string]any{}),
  tool("rule_set","Store a personal constraint or preference; only structured rules are mechanically enforced",map[string]any{"key":prop("string"),"text":prop("string"),"kind":prop("string")},"key","text","kind"),
  tool("rule_list","List personal rules",map[string]any{}),
  tool("weekly_report","Summarize recorded task completions",map[string]any{"week_of":prop("string")}),
  tool("task_review","Inspect recorded deadlines, prerequisites and missing estimates without interpreting absent logs as neglect",map[string]any{"horizon_days":prop("integer")}),
  tool("activity_recent","List confirmed personal activity from canonical records, without fabricating unobserved actions",map[string]any{"limit":prop("integer")}),
  tool("workflow_plan","Propose a model-generated dependency plan for owner review without execution",map[string]any{"project":prop("string"),"objective":prop("string"),"personal_read":prop("boolean")},"project","objective"),
  tool("workflow_submit","Submit a scoped workflow; task creation requires explicit personal_write consent",map[string]any{"project":prop("string"),"objective":prop("string"),"personal_read":prop("boolean"),"personal_write":prop("boolean"),"auto_replan":prop("boolean"),"share_project_evidence":prop("boolean"),"steps":map[string]any{"type":"array","items":prop("object")}},"project","objective","steps"),
  tool("automation_create","Schedule a read-only project workflow with optional private change notifications",map[string]any{"program":prop("object"),"every_minutes":prop("integer"),"enabled":prop("boolean"),"notify_on_change":prop("boolean")},"program","every_minutes"),
  tool("automation_list","List persisted owner-configured workflows and next occurrences",map[string]any{}),
  tool("automation_get","Inspect one scheduled workflow and its last run identity",map[string]any{"id":prop("string")},"id"),
  tool("automation_set_enabled","Revision-fence a recurring workflow pause or resume",map[string]any{"id":prop("string"),"revision":prop("integer"),"enabled":prop("boolean")},"id","revision","enabled"),
  tool("automation_alerts","List confirmed changes to scheduled read results without disclosing the observations",map[string]any{}),
  tool("automation_alert_acknowledge","Acknowledge an observed change with an exact automation revision",map[string]any{"automation_id":prop("string"),"workflow_id":prop("string"),"revision":prop("integer")},"automation_id","workflow_id","revision"),
  tool("workflow_list","List owner-scoped workflow status",map[string]any{}),
  tool("workflow_archive_list","List settled capability workflow history without exposing recorded evidence",map[string]any{}),
  tool("research_learning_preview","Preview source-verified project research for learning without changing learner records",map[string]any{"project_id":prop("string"),"title":prop("string"),"objective":prop("string"),"passage_ids":map[string]any{"type":"array","items":prop("string")}},"project_id","title","passage_ids"),
  tool("research_learning_import","Import exact owner-confirmed research evidence into Learning source records without awarding mastery",map[string]any{"project_id":prop("string"),"title":prop("string"),"objective":prop("string"),"passage_ids":map[string]any{"type":"array","items":prop("string")},"expected_sha256":prop("string"),"confirm":prop("boolean")},"project_id","title","passage_ids","expected_sha256","confirm"),
  tool("workflow_get","Inspect confirmed stage results and status",map[string]any{"id":prop("string")},"id"),
  tool("workflow_cancel","Cancel a revision-matched workflow",map[string]any{"id":prop("string"),"revision":prop("integer")},"id","revision"),
  tool("project_overview","Read a project with linked sources, libraries, personal tasks, goals and engineering runs",map[string]any{"project_id":prop("string")},"project_id"),
  tool("resource_get","Resolve a canonical local resource identity from its owning store",map[string]any{"kind":prop("string"),"id":prop("string")},"kind","id"),
  tool("resource_resolve","Resolve a homelab:kind:id reference without copying the owning service's data",map[string]any{"ref":prop("string")},"ref"),
  tool("system_status","List configured integrations",map[string]any{}),
  tool("device_inventory","List declared hardware access and constraints; this is not an online device probe",map[string]any{}),
  tool("node_inventory","List declarative management modes and roles without probing machines",map[string]any{}),
 }
 if s.engineering!=nil{
  tools=append(tools,
   tool("engineering_run_submit","Submit a bounded engineering run that creates an isolated workspace, edits approved files, verifies checks and prepares a draft pull request",map[string]any{"project":prop("string"),"repository":prop("string"),"revision":prop("string"),"base_branch":prop("string"),"expected_head":prop("string"),"objective":prop("string"),"context_paths":map[string]any{"type":"array","items":prop("string")},"allowed_paths":map[string]any{"type":"array","items":prop("string")},"changes":prop("object"),"checks":map[string]any{"type":"array","items":prop("string")},"image_pool_id":prop("string"),"cpu":prop("number"),"memory_mb":prop("integer"),"duration_minutes":prop("integer"),"title":prop("string"),"description":prop("string"),"autonomous":prop("boolean"),"review":prop("boolean"),"include_knowledge":prop("boolean"),"prepare_only":prop("boolean"),"linked_repositories":map[string]any{"type":"array","items":prop("object")}},"project","repository","revision","base_branch","expected_head","objective","checks","image_pool_id","cpu","memory_mb","duration_minutes","title"),
   tool("engineering_run_list","List engineering runs and confirmed outcomes",map[string]any{}),
   tool("engineering_run_get","Inspect one engineering run without returning lease secrets",map[string]any{"id":prop("string")},"id"),
   tool("engineering_run_cancel","Cancel a revision-matched engineering run",map[string]any{"id":prop("string"),"revision":prop("integer")},"id","revision"))
 }
 if _,ok:=s.engineering.(agentflow.TeamManager);ok{
  tools=append(tools,
   tool("engineering_team_submit","Start bounded cooperating agents with disjoint editable files; no member may publish",map[string]any{"base":prop("object"),"tasks":map[string]any{"type":"array","items":prop("object")}},"base","tasks"),
   tool("engineering_team_list","List cooperating engineering agent groups",map[string]any{}),
   tool("engineering_team_get","Inspect one cooperating engineering group",map[string]any{"id":prop("string")},"id"),
   tool("engineering_team_cancel","Cancel unfinished members of a cooperating engineering group",map[string]any{"id":prop("string")},"id"),
   tool("engineering_team_integrate","Verify the combined agent artifacts and prepare one draft PR",map[string]any{"id":prop("string"),"base_branch":prop("string"),"expected_head":prop("string"),"title":prop("string"),"body":prop("string")},"id","base_branch","expected_head","title"))
 }
 if _,ok:=s.engineering.(agentflow.KnowledgeManager);ok{
  tools=append(tools,
   tool("engineering_run_knowledge_get","Inspect a source-backed knowledge proposal",map[string]any{"id":prop("string"),"proposal_id":prop("string")},"id","proposal_id"),
   tool("engineering_run_knowledge_approve","Approve one completed-run knowledge proposal for canonical project storage",map[string]any{"id":prop("string"),"proposal_id":prop("string")},"id","proposal_id"))
 }
 if _,ok:=s.engineering.(agentflow.DiffManager);ok{
  tools=append(tools,tool("engineering_run_diffs","Inspect bounded private patch previews for a scoped engineering run",map[string]any{"id":prop("string")},"id"))
 }
 if s.executorClient!=nil {
  tools=append(tools,
   tool("infrastructure__workspace_stop","Stop an allowlisted Workspace project instance via the privileged Workspace bridge",map[string]any{"host":prop("string"),"project_id":prop("string"),"workspace_id":prop("string")},"host","project_id","workspace_id"),
   tool("infrastructure__workspace_build_environment","Queue an approved project environment build in Workspace",map[string]any{"host":prop("string"),"project_id":prop("string"),"workspace_id":prop("string"),"name":prop("string")},"host","project_id","workspace_id","name"),
   tool("infrastructure__workspace_register_software","Register an operator-approved immutable Workspace package collection",map[string]any{"host":prop("string"),"image_id":prop("string"),"software_id":prop("string"),"packages":map[string]any{"type":"array","items":prop("string")},"description":prop("string")},"host","image_id","software_id","packages"),
   tool("infrastructure__workspace_revoke_worker","Revoke one operator-approved Workspace worker identity",map[string]any{"host":prop("string"),"node_id":prop("string")},"host","node_id"),
   tool("infrastructure__service_start","Start one configured non-critical local service through the independent executor",map[string]any{"host":prop("string"),"service":prop("string")},"host","service"),
   tool("infrastructure__service_stop","Stop one configured non-critical local service through the independent executor",map[string]any{"host":prop("string"),"service":prop("string")},"host","service"),
   tool("infrastructure__service_reload","Reload one configured non-critical local service through the independent executor",map[string]any{"host":prop("string"),"service":prop("string")},"host","service"),
   tool("infrastructure__service_restart","Restart one configured local service through the independent executor",map[string]any{"host":prop("string"),"service":prop("string")},"host","service"),
   tool("infrastructure__release_activate","Activate a staged and verified local generation with rollback through the independent executor",map[string]any{"host":prop("string"),"release_id":prop("string")},"host","release_id"),
   tool("infrastructure__release_revert","Restore the previous verified local generation from the active release journal",map[string]any{"host":prop("string"),"release_id":prop("string")},"host","release_id"),
   tool("cluster__deployment_restart","Restart an explicitly allowlisted Kubernetes deployment through the independent executor",map[string]any{"host":prop("string"),"namespace":prop("string"),"deployment":prop("string")},"host","namespace","deployment"),
   tool("cluster__deployment_scale","Scale an explicitly allowlisted Kubernetes deployment with a bounded replica count",map[string]any{"host":prop("string"),"namespace":prop("string"),"deployment":prop("string"),"replicas":prop("integer")},"host","namespace","deployment","replicas"))
 }
 if s.fleetClient!=nil {
  tools=append(tools,
   tool("infrastructure__fleet_stage","Stage one exact Git commit across the configured fleet without activating any host",map[string]any{"revision":prop("string")},"revision"),
   tool("infrastructure__fleet_plan","Inspect the private validated fleet stage plan",map[string]any{}),
   tool("infrastructure__fleet_prepare","Inspect the exact host confirmations required by a staged fleet plan",map[string]any{"plan_id":prop("string")},"plan_id"),
   tool("infrastructure__fleet_activate","Activate the immutable prepared fleet plan through independent host authorization",map[string]any{"plan_id":prop("string")},"plan_id"),
   tool("infrastructure__fleet_status","Inspect durable fleet transaction status",map[string]any{}),
   tool("infrastructure__fleet_recover","Reconcile unknown release results and recover by exact transaction identity",map[string]any{"transaction_id":prop("string")},"transaction_id"),
   tool("infrastructure__fleet_clear","Clear a resolved fleet transaction and its prepared plan",map[string]any{"plan_id":prop("string")},"plan_id"))
 }
 if s.privilegeClient!=nil {
  tools=append(tools,tool("security_session_status","Inspect the independent owner administrative session without revealing credentials",map[string]any{}),
    tool("security_session_close","Immediately revoke the active owner administrative session",map[string]any{}))
  if s.securityLoginURL!="" {
   tools=append(tools,tool("security_session_open","Open the independent HTTPS authentication page to enter password and TOTP directly, outside ChatGPT. No credentials are returned to the model.",map[string]any{}))
  }
 } else {
  tools=append(tools,tool("approval_status","Check an administrator approval",map[string]any{"id":prop("string")},"id"))
 }
 if s.hasBackend("workspace") {tools=append(tools,tool("software_catalog","List approved images, software collections, and program requests",map[string]any{}))}
 if s.profiles!=nil {tools=append(tools,
  tool("compute_profile_list","List saved reusable build configurations",map[string]any{}),
  tool("compute_profile_get","Read a saved build configuration revision",map[string]any{"id":prop("string"),"version":prop("integer")},"id"),
  tool("compute_profile_save","Save an immutable build profile revision",map[string]any{"spec":prop("object"),"expected_version":prop("integer")},"spec","expected_version"),
  tool("compute_profile_plan","Check available Workspace resources for a saved profile without scheduling",map[string]any{"id":prop("string"),"version":prop("integer"),"project_id":prop("string"),"node_id":prop("string")},"id","project_id"))}
 if s.recipes!=nil {tools=append(tools,
  tool("recipe_import_url_preview","Fetch a public HTTPS recipe page and return an unsaved schema.org recipe draft",map[string]any{"url":prop("string")},"url"),
  tool("recipe_sage_preview","Inspect a RecipeSage JSON-LD export without changing records",map[string]any{"source":prop("string")},"source"),
  tool("recipe_sage_import","Import selected valid RecipeSage JSON-LD recipes using an unchanged preview digest",map[string]any{"source":prop("string"),"digest":prop("string"),"keys":map[string]any{"type":"array","items":prop("string")},"overrides":prop("object")},"source","digest","keys"),
  tool("recipe_list","Find saved recipes",map[string]any{"query":prop("string")}),
  tool("recipe_get","Read a recipe",map[string]any{"id":prop("string")},"id"),
  tool("recipe_history","List current and previous recipe revisions",map[string]any{"id":prop("string")},"id"),
  tool("recipe_restore","Restore a prior recipe version as a new revision",map[string]any{"id":prop("string"),"source_revision":prop("integer"),"expected_revision":prop("integer")},"id","source_revision","expected_revision"),
  tool("recipe_save","Create or revise a recipe with revision control",map[string]any{"recipe":prop("object")},"recipe"),
  tool("recipe_delete","Delete an unscheduled recipe at its expected revision",map[string]any{"id":prop("string"),"revision":prop("integer")},"id","revision"),
  tool("recipe_meals","List scheduled meals",map[string]any{}),
  tool("recipe_meal_schedule","Schedule a recipe for a date",map[string]any{"meal":prop("object")},"meal"),
  tool("recipe_meal_remove","Remove a scheduled meal",map[string]any{"id":prop("string")},"id"),
  tool("recipe_groceries","Consolidate meal-plan ingredients by name and unit",map[string]any{"start":prop("string"),"end":prop("string")}),
  tool("recipe_grocery_needs","Estimate meal-plan grocery quantities after recorded pantry supplies; read-only",map[string]any{"start":prop("string"),"end":prop("string")}),
  tool("recipe_nutrition_preview","Preview reported or estimated recipe nutrients without logging consumption",map[string]any{"id":prop("string"),"servings":prop("number")},"id","servings"),
  tool("recipe_usda_preview","Estimate complete nutrients for owner-reviewed USDA food IDs and edible gram amounts without writing records",map[string]any{"id":prop("string"),"expected_revision":prop("integer"),"ingredients":map[string]any{"type":"array","items":prop("object")}},"id","expected_revision","ingredients"),
  tool("recipe_usda_apply","Save an exact digest-confirmed USDA nutrient estimate as a revisioned recipe, without logging consumption",map[string]any{"id":prop("string"),"expected_revision":prop("integer"),"ingredients":map[string]any{"type":"array","items":prop("object")},"expected_sha256":prop("string"),"confirm":prop("boolean")},"id","expected_revision","ingredients","expected_sha256","confirm"),
  tool("recipe_pantry_list","List recorded pantry inventory and revision without inferring unrecorded stock",map[string]any{}),
  tool("recipe_pantry_set","Store a pantry quantity with revision-checked updates",map[string]any{"item":prop("object"),"expected_revision":prop("integer")},"item","expected_revision"),
  tool("recipe_pantry_remove","Remove a pantry entry using an exact revision",map[string]any{"key":prop("string"),"expected_revision":prop("integer")},"key","expected_revision"),
  tool("recipe_recommend","Rank recipes by recorded supplies and explicit nutritional targets without logging meals",map[string]any{"servings":prop("number"),"targets":map[string]any{"type":"array","items":prop("object")},"limit":prop("integer")}))}
 if s.bookCatalog!=nil {tools=append(tools,
  tool("book_search","Discover published books through the official Open Library catalog",map[string]any{"query":prop("string")},"query"),
   tool("book_lending","Look up provider-authorized online reading and borrowing links for an exact ISBN; does not borrow or download books",map[string]any{"isbn":prop("string")},"isbn"))}
 tools=append(tools,
  tool("book_request_list","List personal book wishes without claiming provider acquisition",map[string]any{"status":prop("string")}),
  tool("book_request_add","Record an independently identified book wish with format and revision",map[string]any{"request":prop("object")},"request"),
  tool("book_request_status","Record manual fulfillment or cancellation of a pending book wish",map[string]any{"id":prop("string"),"status":prop("string"),"expected_revision":prop("integer")},"id","status","expected_revision"))
 if s.calendarURL!=""{tools=append(tools,
  tool("calendar_feed","Read read-only calendar event definitions",map[string]any{}),
  tool("calendar_occurrences","Expand supported iCalendar recurrences into a bounded time window with explicit limitations",map[string]any{"start":prop("string"),"end":prop("string")}))}
 tools=append(tools,tool("weekly_plan","Propose task timeboxes from recorded durations, prerequisites and calendar reservations without changing any records",map[string]any{"from":prop("string"),"daily_minutes":prop("integer"),"start_hour":prop("integer")}))
 if s.observability!=nil {tools=append(tools,
  tool("monitoring_summary","Inspect measured CPU, memory, disk, GPU and collector telemetry without inventing missing readings",map[string]any{}),
  tool("monitoring_targets","Inspect Prometheus collector availability and last scrape reports",map[string]any{}),
  tool("monitoring_alerts","List Prometheus firing and pending alert rules",map[string]any{}),
  tool("monitoring_security","Summarize security authentication and revocation signals from collected journal events",map[string]any{}),
  tool("monitoring_series","Inspect a selected fixed monitoring metric and bounded time range",map[string]any{"metric":prop("string"),"range":prop("string")},"metric"),
  tool("monitoring_logs","Read a bounded Loki system journal with literal search text",map[string]any{"range":prop("string"),"search":prop("string"),"limit":prop("integer")}))}

 if s.media!=nil{tools=append(tools,
  tool("media_search","Search movies and television through Seerr",map[string]any{"query":prop("string")},"query"),
  tool("media_requests","List Seerr media requests",map[string]any{}),
  tool("media_request","Request a movie or television season through Seerr",map[string]any{"mediaType":prop("string"),"mediaId":prop("integer"),"seasons":map[string]any{"type":"array","items":prop("integer")},"is4k":prop("boolean")},"mediaType","mediaId"))}
 return tools
}
// Tool discovery uses one end-to-end deadline. Failed integrations do not
// delay or suppress local tools, and results retain configured backend order.
func(s *Server)toolList(r *http.Request)[]map[string]any{
 out:=s.localTools()
 if s.privilegeClient!=nil&&s.securityLoginURL!="" {
  for _,item:=range out {
   if item["name"]=="security_session_open"{
    item["_meta"]=map[string]any{
     "ui":map[string]any{"resourceUri":"ui://homelab/security-login.html"},
     "openai/outputTemplate":"ui://homelab/security-login.html",
    }
   }
  }
 }
 if len(s.backend)==0{return out}
 type discovery struct{
  index int
  tools []map[string]any
 }
 ctx,cancel:=context.WithTimeout(r.Context(),4*time.Second)
 defer cancel()
 results:=make(chan discovery,len(s.backend))
 for index,backend:=range s.backend{
  go func(index int,backend Backend){
   response:=discovery{index:index}
   defer func(){results<-response}()
   var remote any
   if err:=s.forward(ctx,backend,rpcRequest{
    JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/list",
   },&remote);err!=nil{return}
   value,ok:=remote.(map[string]any)
   if !ok{return}
   list,ok:=value["tools"].([]any)
   if !ok||len(list)>512{return}
   for _,item:=range list{
    advertised,ok:=item.(map[string]any)
    if !ok{continue}
    name,ok:=advertised["name"].(string)
    if !ok||!validBackendToolName(name){continue}
    if s.privilegeClient!=nil&&approval.ScopeFor(backend.Name+"__"+name)!=""{continue}
    if backend.Name=="workspace"&&(
     name=="github_merge_pull_request"||
     name=="github_mark_pull_request_ready"||
     name=="github_repository_api"||
     name=="github_cleanup_merged_branch"){
     continue
    }
    namespace:=backend.Name
    if namespace=="workspace"{namespace="compute"}
    // Each backend response owns its decoded objects; never mutate a
    // shared registered schema while normalizing its exported identity.
    named:=make(map[string]any,len(advertised))
    for key,value:=range advertised{named[key]=value}
    named["name"]=namespace+"__"+name
    response.tools=append(response.tools,named)
   }
  }(index,backend)
 }
 ordered:=make([][]map[string]any,len(s.backend))
 remaining:=len(s.backend)
 for remaining>0{
  select{
  case discovered:=<-results:
   ordered[discovered.index]=discovered.tools
   remaining--
  case <-ctx.Done():
   remaining=0
  }
 }
 seen:=map[string]bool{}
 for _,item:=range out{
  if name,ok:=item["name"].(string);ok{seen[name]=true}
 }
 for _,group:=range ordered{
  for _,item:=range group{
   name:=item["name"].(string)
   if seen[name]{continue}
   seen[name]=true
   out=append(out,item)
  }
 }
 return out
}
func(s *Server)localTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
 if name=="task_decompose" {
  var request struct{
   ParentID string `json:"parent_id"`
   ExpectedRevision int `json:"expected_revision"`
   Steps []store.TaskStepInput `json:"steps"`
  }
  if err:=json.Unmarshal(raw,&request);err!=nil{return nil,errors.New("invalid decomposition arguments")}
  return s.db.DecomposeTask(request.ParentID,request.ExpectedRevision,request.Steps)
 }
 if name=="research_learning_preview"||name=="research_learning_import"{return s.researchLearningTool(ctx,name,raw)}
 if strings.HasPrefix(name,"automation_"){return s.capabilityAutomationTool(ctx,name,raw)}
 if strings.HasPrefix(name,"workflow_"){return s.capabilityWorkflowTool(ctx,name,raw)}
 if strings.HasPrefix(name,"engineering_run_")||strings.HasPrefix(name,"engineering_team_"){return s.engineeringTool(ctx,name,raw)}
 if strings.HasPrefix(name,"monitoring_"){return s.monitoringTool(ctx,name,raw)}
 if strings.HasPrefix(name,"book_"){return s.bookTool(ctx,name,raw)}
 if name=="calendar_occurrences"||name=="weekly_plan"{return s.calendarTool(ctx,name,raw)}
 if strings.HasPrefix(name,"recipe_"){return s.recipeTool(ctx,name,raw)}
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
  Reference string `json:"ref"`
  HorizonDays int `json:"horizon_days"`
 }
 if len(raw)>0{if e:=json.Unmarshal(raw,&args);e!=nil{return nil,e}}
 switch name{
 case "task_add":return s.db.AddTask(args.TaskInput)
 case "task_complete":return s.db.CompleteTask(args.ID)
 case "task_list":return s.db.Tasks(args.Status),nil
 case "task_delegations":return s.taskDelegations(),nil
 case "task_suggest":return s.db.Suggest(args.Minutes,args.Energy,args.Location,args.Mode,args.Limit),nil
 case "goal_add":return s.db.AddGoal(args.Title,args.Notes)
 case "goal_list":return s.db.Goals(),nil
 case "rule_set":return s.db.SetRule(args.Key,args.Text,args.Kind)
 case "rule_list":return s.db.Rules(),nil
 case "weekly_report":return s.db.Report(args.WeekOf)
 case "task_review":
  n:=args.HorizonDays
  if n==0{n=7}
  return s.db.ReviewTasks(time.Now(),n)
 case "activity_recent":
  n:=args.Limit
  if n==0{n=50}
  if n<1||n>200{return nil,errors.New("activity limit must be 1 through 200")}
  return s.activity(n,time.Time{}),nil
 case "project_overview":
  var request struct{ProjectID string `json:"project_id"`}
  if err:=json.Unmarshal(raw,&request);err!=nil{return nil,err}
  return s.projectOverview(ctx,request.ProjectID)
 case "resource_get":return s.resource(ctx,args.Kind,args.ID)
 case "resource_resolve":
  kind,id,err:=parseResourceReference(args.Reference)
  if err!=nil{return nil,err}
  return s.resource(ctx,kind,id)
 case "approval_status":return s.privilegeStatus(args.ID)
 case "security_session_status":
  if s.privilegeClient==nil{return nil,errors.New("independent security service not configured")}
  return s.privilegeClient.Status(ctx)
 case "security_session_open":
  if s.privilegeClient==nil||s.securityLoginURL==""{return nil,errors.New("browser authentication not configured")}
  status,err:=s.privilegeClient.Status(ctx)
  if err!=nil{return nil,err}
  return map[string]any{
   "sign_in_url":s.securityLoginURL,
   "session":status,
   "instructions":"Open the protected HTTPS link in your browser to authenticate. Password and TOTP are entered directly into the independent service, never into a ChatGPT message or MCP tool. After authentication, retry the operation.",
  },nil
 case "security_session_close":
  if s.privilegeClient==nil{return nil,errors.New("independent security service not configured")}
  if err:=s.privilegeClient.Revoke(ctx);err!=nil{return nil,err}
  return map[string]bool{"active":false},nil
 case "device_inventory":return s.deviceCatalog.List(),nil
 case "node_inventory":return s.nodeCatalog.List(),nil
 case "calendar_feed":
  if s.calendarURL==""{return nil,errors.New("calendar is not configured")}
  return calendar.Fetch(ctx,s.client,s.calendarURL)
 case "software_catalog":return s.softwareEntries(ctx)
 case "system_status":
  statuses:=map[string]string{}
  type observation struct{
   name string
   available bool
  }
  checkCtx,cancel:=context.WithTimeout(ctx,4*time.Second)
  defer cancel()
  completed:=make(chan observation,len(s.backend))
  for _,backend:=range s.backend{
   namespace:=backend.Name
   if namespace=="workspace"{namespace="compute"}
   statuses[namespace]="unavailable"
   go func(b Backend,name string){
    var output any
    err:=s.forward(checkCtx,b,rpcRequest{
     JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/list",
    },&output)
    reply,ok:=output.(map[string]any)
    _,hasList:=reply["tools"].([]any)
    completed<-observation{name:name,available:err==nil&&ok&&hasList}
   }(backend,namespace)
  }
  remaining:=len(s.backend)
  for remaining>0{
   select{
   case result:=<-completed:
    if result.available{statuses[result.name]="available"}
    remaining--
   case <-checkCtx.Done():
    remaining=0
   }
  }
  return map[string]any{"integrations":statuses,"local":"available"},nil
 default:return nil,fmt.Errorf("unknown tool: %s",name)
 }
}
