package transport

import (
 "net/http"
 "strconv"
 "time"
)

func(s *Server) uiTaskReview(w http.ResponseWriter,r *http.Request){
 days:=7
 if raw:=r.URL.Query().Get("days");raw!=""{
  n,err:=strconv.Atoi(raw)
  if err!=nil||n<1||n>30{writeError(w,400,"review horizon must be 1 to 30 days");return}
  days=n
 }
 review,err:=s.db.ReviewTasks(time.Now(),days)
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,200,review)
}
