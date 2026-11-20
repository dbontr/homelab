package store

import (
 "errors"
 "os"
 "sort"
 "strings"
 "time"
)

// Notifications is the single, durable, source-attributed inbox. Muting a
// source affects presentation, never event retention or the underlying audit.
type NotificationEvent struct {
 ID string `json:"id"`
 Source string `json:"source"`
 EventKey string `json:"event_key"`
 Severity string `json:"severity"`
 Title string `json:"title"`
 Message string `json:"message"`
 CreatedAt time.Time `json:"created_at"`
 AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
}
type NotificationPreference struct {
 Source string `json:"source"`
 Enabled bool `json:"enabled"`
 UpdatedAt time.Time `json:"updated_at,omitempty"`
}
var notificationSources = []string{
 "automation", "backup", "compute", "devices", "engineering", "health",
 "learning", "media", "monitoring", "motivation", "network", "personal",
 "security", "software", "system",
}
func NotificationSources() []string { return append([]string(nil),notificationSources...) }
func ValidNotificationSource(source string) bool {
 for _,name:=range notificationSources { if name==source{return true} }
 return false
}
func notificationValidKey(key string)bool{
 if len(key)<1||len(key)>128{return false}
 for _,c:=range key {
  if c>='a'&&c<='z'||c>='A'&&c<='Z'||c>='0'&&c<='9'||c=='_'||c=='-'||c=='.'||c==':'||c=='/' {continue}
  return false
 }
 return true
}

// PublishNotification is idempotent by (source,eventKey); no untrusted request
// may select its own source without a matching service identity at the HTTP edge.
func(s *Store) PublishNotification(source,eventKey,severity,title,message string)(NotificationEvent,error){
 title=strings.TrimSpace(title); message=strings.TrimSpace(message)
 if !ValidNotificationSource(source)||!notificationValidKey(eventKey) {return NotificationEvent{},errors.New("invalid notification source or event key")}
 if severity!="info"&&severity!="warning"&&severity!="critical"{return NotificationEvent{},errors.New("invalid notification severity")}
 if len(title)<1||len(title)>180||strings.ContainsAny(title,"\x00\r\n")||len(message)>2000||strings.ContainsRune(message,0) {
  return NotificationEvent{},errors.New("invalid notification content")
 }
 s.mu.Lock();defer s.mu.Unlock()
 for _,prior:=range s.data.Notifications{
  if prior.Source!=source||prior.EventKey!=eventKey {continue}
  if prior.Severity!=severity||prior.Title!=title||prior.Message!=message {
   return NotificationEvent{},errors.New("notification event key conflicts with existing event")
  }
  return prior,nil
 }
 next:=clone(s.data)
 if len(next.Notifications)>=1024 {
  var oldest NotificationEvent
  for _,event:=range next.Notifications{
   if event.AcknowledgedAt==nil {continue}
   if oldest.ID==""||event.CreatedAt.Before(oldest.CreatedAt){oldest=event}
  }
  if oldest.ID==""{return NotificationEvent{},errors.New("notification inbox capacity reached; acknowledge existing events")}
  delete(next.Notifications,oldest.ID)
 }
 uid,err:=id();if err!=nil{return NotificationEvent{},err}
 event:=NotificationEvent{ID:uid,Source:source,EventKey:eventKey,Severity:severity,Title:title,Message:message,CreatedAt:time.Now().UTC()}
 next.Notifications[event.ID]=event
 return event,s.save(next)
}
func(s *Store) Notifications(limit int)[]NotificationEvent{
 s.mu.Lock();defer s.mu.Unlock()
 if limit<=0||limit>1024{limit=200}
 out:=make([]NotificationEvent,0,len(s.data.Notifications))
 for _,event:=range s.data.Notifications{out=append(out,event)}
 sort.Slice(out,func(i,j int)bool{return out[i].CreatedAt.After(out[j].CreatedAt)})
 if len(out)>limit{out=out[:limit]}
 return out
}
func(s *Store) NotificationPreferences()[]NotificationPreference{
 s.mu.Lock();defer s.mu.Unlock()
 out:=make([]NotificationPreference,0,len(notificationSources))
 for _,source:=range notificationSources{
  if pref,ok:=s.data.NotificationPrefs[source];ok {out=append(out,pref)
  }else{out=append(out,NotificationPreference{Source:source,Enabled:true})}
 }
 return out
}
func(s *Store) SetNotificationPreference(source string,enabled bool)(NotificationPreference,error){
 if !ValidNotificationSource(source){return NotificationPreference{},errors.New("invalid notification source")}
 s.mu.Lock();defer s.mu.Unlock()
 pref:=NotificationPreference{Source:source,Enabled:enabled,UpdatedAt:time.Now().UTC()}
 next:=clone(s.data);next.NotificationPrefs[source]=pref
 return pref,s.save(next)
}
func(s *Store) AcknowledgeNotification(uid string)(NotificationEvent,error){
 s.mu.Lock();defer s.mu.Unlock()
 event,ok:=s.data.Notifications[uid]
 if !ok {return NotificationEvent{},os.ErrNotExist}
 if event.AcknowledgedAt!=nil{return event,nil}
 at:=time.Now().UTC();event.AcknowledgedAt=&at
 next:=clone(s.data);next.Notifications[uid]=event
 return event,s.save(next)
}
