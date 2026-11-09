package personal

import (
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "strconv"
 "time"

 "github.com/dbontr/homelab-new/system/store"
)

// Handler owns the task, goal, rule and report HTTP contracts. Admission
// remains the responsibility of the trusted private service router.
type Handler struct { db *store.Store }

// Register attaches personal application routes to the authenticated router.
// Both HTTP and MCP operations use the same durable store, without a second
// writer or a second copy of personal records.
func Register(mux *http.ServeMux, db *store.Store) {
 if mux==nil || db==nil { panic("personal application requires its store and router") }
 h:=&Handler{db:db}
 mux.HandleFunc("GET /v1/tasks",h.tasks)
 mux.HandleFunc("POST /v1/tasks",h.tasks)
 mux.HandleFunc("POST /v1/tasks/{id}/complete",h.complete)
 mux.HandleFunc("POST /v1/tasks/{id}/decompose",h.taskDecompose)
 mux.HandleFunc("POST /v1/tasks/capture",h.taskCapture)
 mux.HandleFunc("GET /v1/tasks/agenda",h.taskAgenda)
 mux.HandleFunc("PUT /v1/tasks/{id}",h.taskUpdate)
 mux.HandleFunc("GET /v1/goals",h.goals)
 mux.HandleFunc("POST /v1/goals",h.goals)
 mux.HandleFunc("GET /v1/rules",h.rules)
 mux.HandleFunc("PUT /v1/rules/{key}",h.rules)
 mux.HandleFunc("GET /v1/recommendations",h.recommendations)
 mux.HandleFunc("GET /v1/reports/weekly",h.weekly)
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

func(h *Handler) tasks(w http.ResponseWriter,r *http.Request){
 if r.Method=="GET"{writeJSON(w,200,h.db.Tasks(r.URL.Query().Get("status")));return}
 var in store.TaskInput
 if e:=decode(r,&in);e!=nil{writeError(w,400,e.Error());return}
 t,e:=h.db.AddTask(in);if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,201,t)
}
func(h *Handler) complete(w http.ResponseWriter,r *http.Request){
 t,e:=h.db.CompleteTask(r.PathValue("id"))
 if errors.Is(e,os.ErrNotExist){writeError(w,404,"task not found");return}
 if errors.Is(e,store.ErrTaskChildrenIncomplete){writeError(w,409,"complete recorded subtasks before the parent");return}
 if e!=nil{writeError(w,500,"task completion failed");return}
 writeJSON(w,200,t)
}
func(h *Handler) goals(w http.ResponseWriter,r *http.Request){
 if r.Method=="GET"{writeJSON(w,200,h.db.Goals());return}
 var in struct {Title string `json:"title"`; Notes string `json:"notes"`}
 if e:=decode(r,&in);e!=nil{writeError(w,400,e.Error());return}
 g,e:=h.db.AddGoal(in.Title,in.Notes);if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,201,g)
}
func(h *Handler) rules(w http.ResponseWriter,r *http.Request){
 if r.Method=="GET"{writeJSON(w,200,h.db.Rules());return}
 var in struct {Text string `json:"text"`; Kind string `json:"kind"`}
 if e:=decode(r,&in);e!=nil{writeError(w,400,e.Error());return}
 v,e:=h.db.SetRule(r.PathValue("key"),in.Text,in.Kind);if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,200,v)
}
func(h *Handler) recommendations(w http.ResponseWriter,r *http.Request){
 q:=r.URL.Query();minutes,_:=strconv.Atoi(q.Get("minutes"));limit,_:=strconv.Atoi(q.Get("limit"))
 writeJSON(w,200,h.db.Suggest(minutes,q.Get("energy"),q.Get("location"),q.Get("mode"),limit))
}
func(h *Handler) weekly(w http.ResponseWriter,r *http.Request){
 v,e:=h.db.Report(r.URL.Query().Get("week_of"));if e!=nil{writeError(w,400,e.Error());return}
 writeJSON(w,200,v)
}

func(h *Handler) taskDecompose(w http.ResponseWriter,r *http.Request){
 var input struct{
  ExpectedRevision int `json:"expected_revision"`
  Steps []store.TaskStepInput `json:"steps"`
 }
 if err:=decode(r,&input);err!=nil{writeError(w,400,"invalid decomposition request");return}
 result,err:=h.db.DecomposeTask(r.PathValue("id"),input.ExpectedRevision,input.Steps)
 switch {
 case errors.Is(err,os.ErrNotExist):writeError(w,404,"parent task not found")
 case errors.Is(err,store.ErrTaskRevision):writeError(w,409,"parent task revision conflict")
 case err!=nil:writeError(w,400,err.Error())
 default:writeJSON(w,http.StatusCreated,result)
 }
}

func(h *Handler) taskCapture(w http.ResponseWriter,r *http.Request){
 var request struct{ Text string `json:"text"` }
 if err:=decode(r,&request);err!=nil{writeError(w,400,err.Error());return}
 task,err:=h.db.Capture(request.Text)
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,http.StatusCreated,task)
}

func(h *Handler) taskAgenda(w http.ResponseWriter,r *http.Request){
 day:=r.URL.Query().Get("date")
 if day==""{day=time.Now().Format("2006-01-02")}
 rows,err:=h.db.Agenda(day)
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,200,rows)
}

func(h *Handler) taskUpdate(w http.ResponseWriter,r *http.Request){
 var change store.TaskChanges
 if err:=decode(r,&change);err!=nil{writeError(w,400,err.Error());return}
 task,err:=h.db.UpdateTask(r.PathValue("id"),change)
 switch {
 case errors.Is(err,os.ErrNotExist):writeError(w,404,"task not found")
 case errors.Is(err,store.ErrTaskRevision):writeError(w,409,"task revision conflict")
 case err!=nil:writeError(w,400,err.Error())
 default:writeJSON(w,200,task)
 }
}
