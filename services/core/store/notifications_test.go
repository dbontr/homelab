package store

import (
 "os"
 "testing"
)

func TestNotificationsPersistDeduplicateAndMute(t *testing.T){
 s,path:=testStore(t)
 event,err:=s.PublishNotification("engineering","build-17","warning","Build requires attention","Compiler was unavailable")
 if err!=nil{t.Fatal(err)}
 again,err:=s.PublishNotification("engineering","build-17","warning","Build requires attention","Compiler was unavailable")
 if err!=nil||again.ID!=event.ID{t.Fatalf("idempotency: %+v %v",again,err)}
 if _,err=s.PublishNotification("engineering","build-17","critical","Different","payload");err==nil{
  t.Fatal("accepted conflicting idempotency key")
 }
 pref,err:=s.SetNotificationPreference("engineering",false)
 if err!=nil||pref.Enabled{t.Fatalf("muting failed %+v %v",pref,err)}
 // Muting is a presentation preference; operational events remain retained.
 if len(s.Notifications(200))!=1{t.Fatal("muting erased the event")}
 acknowledged,err:=s.AcknowledgeNotification(event.ID)
 if err!=nil||acknowledged.AcknowledgedAt==nil{t.Fatalf("acknowledgement: %v",err)}
 reloaded,err:=Open(path,"UTC");if err!=nil{t.Fatal(err)}
 list:=reloaded.Notifications(200)
 if len(list)!=1||list[0].ID!=event.ID||list[0].AcknowledgedAt==nil{t.Fatal("notification state was not durable")}
 for _,p:=range reloaded.NotificationPreferences(){
  if p.Source=="engineering"&&p.Enabled{t.Fatal("mute preference not durable")}
  if p.Source=="security"&&!p.Enabled{t.Fatal("default source must remain visible")}
 }
 info,err:=os.Stat(path);if err!=nil||info.Mode().Perm()!=0600{t.Fatalf("private state file: %v",err)}
}
func TestNotificationValidationAndMissingAcknowledge(t *testing.T){
 s,_:=testStore(t)
 for _,in:=range []struct{source,key,severity,title,message string}{
  {"unknown","a","info","Title","body"},
  {"health","a key","info","Title","body"},
  {"security","a","debug","Title","body"},
  {"security","a","critical","","body"},
 }{
  if _,e:=s.PublishNotification(in.source,in.key,in.severity,in.title,in.message);e==nil{t.Fatal("accepted malformed event",in)}
 }
 if _,e:=s.SetNotificationPreference("unknown",true);e==nil{t.Fatal("accepted unknown source")}
 if _,e:=s.AcknowledgeNotification("not-there");e==nil{t.Fatal("acknowledged unknown notification")}
}
