package transport

import (
 "errors"
 "net/http"
 "os"
 "time"

 "github.com/dbontr/homelab-new/system/store"
)

func(s *Server) taskDecompose(w http.ResponseWriter,r *http.Request){
 var input struct{
  ExpectedRevision int `json:"expected_revision"`
  Steps []store.TaskStepInput `json:"steps"`
 }
 if err:=decode(r,&input);err!=nil{writeError(w,400,"invalid decomposition request");return}
 result,err:=s.db.DecomposeTask(r.PathValue("id"),input.ExpectedRevision,input.Steps)
 switch {
 case errors.Is(err,os.ErrNotExist):writeError(w,404,"parent task not found")
 case errors.Is(err,store.ErrTaskRevision):writeError(w,409,"parent task revision conflict")
 case err!=nil:writeError(w,400,err.Error())
 default:writeJSON(w,http.StatusCreated,result)
 }
}

func (s *Server) taskCapture(w http.ResponseWriter,r *http.Request){
 var request struct{ Text string `json:"text"` }
 if err:=decode(r,&request);err!=nil{writeError(w,400,err.Error());return}
 task,err:=s.db.Capture(request.Text)
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,http.StatusCreated,task)
}

func (s *Server) taskAgenda(w http.ResponseWriter,r *http.Request){
 day:=r.URL.Query().Get("date")
 if day==""{day=time.Now().Format("2006-01-02")}
 rows,err:=s.db.Agenda(day)
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,200,rows)
}

func (s *Server) taskUpdate(w http.ResponseWriter,r *http.Request){
 var change store.TaskChanges
 if err:=decode(r,&change);err!=nil{writeError(w,400,err.Error());return}
 task,err:=s.db.UpdateTask(r.PathValue("id"),change)
 switch {
 case errors.Is(err,os.ErrNotExist):writeError(w,404,"task not found")
 case errors.Is(err,store.ErrTaskRevision):writeError(w,409,"task revision conflict")
 case err!=nil:writeError(w,400,err.Error())
 default:writeJSON(w,200,task)
 }
}
