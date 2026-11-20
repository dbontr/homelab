package transport

import (
 "crypto/subtle"
 "errors"
 "os"
 "net/http"
 "strings"

 "github.com/dbontr/homelab-new/system/store"
)

// SetNotificationPublishers installs explicit per-source credentials. A normal
// API token, browser session or workspace identity cannot impersonate a source.
func(s *Server) SetNotificationPublishers(tokens map[string]string)error{
 if len(s.notificationPublishers)!=0{return errors.New("notification source policy already configured")}
 seen:=map[string]bool{}
 copyTokens:=make(map[string]string,len(tokens))
 for source,token:=range tokens{
  if !store.ValidNotificationSource(source)||len(token)<32||len(token)>1024||
    strings.TrimSpace(token)!=token||strings.ContainsAny(token,"\x00\r\n\t")||
    token==s.token||seen[token]{
   return errors.New("invalid or reused notification publisher credential")
  }
  seen[token]=true
  copyTokens[source]=token
 }
 s.notificationPublishers=copyTokens
 return nil
}
func(s *Server) notificationPublisherAuthorized(r *http.Request)bool{
 if r.Method!=http.MethodPost||r.URL.Path=="" {return false}
 raw:=strings.TrimPrefix(r.URL.Path,"/v1/notifications/publish/")
 if raw==r.URL.Path||!store.ValidNotificationSource(raw){return false}
 token,ok:=s.notificationPublishers[raw]
 if !ok{return false}
 supplied,valid:=strings.CutPrefix(r.Header.Get("Authorization"),"Bearer ")
 return valid&&subtle.ConstantTimeCompare([]byte(supplied),[]byte(token))==1
}
func(s *Server) notificationFeed(w http.ResponseWriter,r *http.Request){
 writeJSON(w,http.StatusOK,map[string]any{"events":s.db.Notifications(200)})
}
func(s *Server) notificationPreferences(w http.ResponseWriter,r *http.Request){
 writeJSON(w,http.StatusOK,map[string]any{"sources":s.db.NotificationPreferences()})
}
func(s *Server) notificationPreferenceUpdate(w http.ResponseWriter,r *http.Request){
 source:=r.PathValue("source")
 if !store.ValidNotificationSource(source){writeError(w,400,"unknown notification source");return}
 var input struct{Enabled *bool `json:"enabled"`}
 r.Body=http.MaxBytesReader(w,r.Body,4096)
 if e:=decode(r,&input);e!=nil||input.Enabled==nil{writeError(w,400,"an enabled boolean is required");return}
 pref,e:=s.db.SetNotificationPreference(source,*input.Enabled)
 if e!=nil{writeError(w,500,"notification preferences could not be saved");return}
 writeJSON(w,200,pref)
}
func(s *Server) notificationAcknowledge(w http.ResponseWriter,r *http.Request){
 event,e:=s.db.AcknowledgeNotification(r.PathValue("id"))
 if errors.Is(e,os.ErrNotExist){writeError(w,404,"notification not found");return}
 if e!=nil{writeError(w,500,"notification could not be acknowledged");return}
 writeJSON(w,200,event)
}
func(s *Server) notificationPublish(w http.ResponseWriter,r *http.Request){
 if !s.notificationPublisherAuthorized(r){writeError(w,401,"source publisher authorization required");return}
 if !strings.HasPrefix(r.Header.Get("Content-Type"),"application/json"){writeError(w,415,"JSON content required");return}
 source:=r.PathValue("source")
 var in struct{
  EventKey string `json:"event_key"`
  Severity string `json:"severity"`
  Title string `json:"title"`
  Message string `json:"message"`
 }
 r.Body=http.MaxBytesReader(w,r.Body,8192)
 if e:=decode(r,&in);e!=nil{writeError(w,400,"invalid notification event");return}
 item,e:=s.db.PublishNotification(source,in.EventKey,in.Severity,in.Title,in.Message)
 if e!=nil{
  if strings.Contains(e.Error(),"conflicts")||strings.Contains(e.Error(),"capacity"){writeError(w,409,e.Error());return}
  writeError(w,400,"invalid notification event");return
 }
 writeJSON(w,200,item)
}
