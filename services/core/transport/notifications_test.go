package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestNotificationPublisherIsolationAndOwnerAPI(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"private","state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 apiToken:=strings.Repeat("A",48)
 s,err:=New(db,apiToken,nil);if err!=nil{t.Fatal(err)}
 securityToken:=strings.Repeat("S",48)
 engineeringToken:=strings.Repeat("E",48)
 if err=s.SetNotificationPublishers(map[string]string{"security":securityToken,"engineering":engineeringToken});err!=nil{t.Fatal(err)}
 h:=s.Handler()
 publish:=func(source,token string) *httptest.ResponseRecorder{
  r:=httptest.NewRequest(http.MethodPost,"/v1/notifications/publish/"+source,strings.NewReader(`{"event_key":"release-33","severity":"warning","title":"Service event","message":"Review release"}`))
  r.Header.Set("Content-Type","application/json")
  if token!=""{r.Header.Set("Authorization","Bearer "+token)}
  w:=httptest.NewRecorder();h.ServeHTTP(w,r);return w
 }
 for _,token:=range []string{"",apiToken,engineeringToken}{
  if w:=publish("security",token);w.Code!=http.StatusUnauthorized{t.Fatalf("source impersonation accepted: %d %s",w.Code,w.Body.String())}
 }
 if w:=publish("security",securityToken);w.Code!=200{t.Fatalf("scoped publisher rejected: %d %s",w.Code,w.Body.String())}
 if w:=publish("security",securityToken);w.Code!=200{t.Fatalf("idempotent replay failed: %d",w.Code)}
 if len(db.Notifications(10))!=1{t.Fatal("idempotent publish duplicated the event")}
 if w:=request(h,"GET","/v1/notifications","","");w.Code!=401{t.Fatal("inbox exposed without authorization")}
 if w:=request(h,"GET","/v1/notifications","",apiToken);w.Code!=200{t.Fatalf("inbox failed: %s",w.Body.String())}
 w:=request(h,http.MethodPut,"/v1/notifications/preferences/engineering",`{"enabled":false}`,apiToken)
 if w.Code!=200{t.Fatalf("preferences write failed: %s",w.Body.String())}
 if w:=request(h,"GET","/v1/notifications/preferences","",apiToken);w.Code!=200{t.Fatalf("preferences failed %s",w.Body.String())}
 event:=db.Notifications(10)[0]
 w=request(h,"POST","/v1/notifications/ack/"+event.ID,`{}`,apiToken)
 if w.Code!=200{t.Fatalf("ack failed: %s",w.Body.String())}
 var ack store.NotificationEvent
 if err=json.Unmarshal(w.Body.Bytes(),&ack);err!=nil||ack.AcknowledgedAt==nil{t.Fatalf("ack not saved: %+v %v",ack,err)}
}
func TestNotificationPolicyRejectsCredentialReuse(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 apiToken:=strings.Repeat("A",48)
 for _,policy:=range []map[string]string{
  {"security":apiToken},
  {"security":"short"},
  {"unknown":strings.Repeat("S",48)},
  {"security":strings.Repeat("S",48),"engineering":strings.Repeat("S",48)},
 }{
  s,err:=New(db,apiToken,nil);if err!=nil{t.Fatal(err)}
  if e:=s.SetNotificationPublishers(policy);e==nil{t.Fatalf("accepted unsafe source policy %#v",policy)}
 }
}
