package mcp

import (
 "encoding/json"
 "path/filepath"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
func privateHealthToolDir(t *testing.T) string {
 t.Helper()
 directory:=t.TempDir()
 if err:=os.Chmod(directory,0700);err!=nil{t.Fatal(err)}
 return directory
}

func healthServer(t *testing.T) *Server {
 t.Helper()
 db,err:=health.Open(filepath.Join(privateHealthToolDir(t),"health.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ =db.Close()})
 return New(nil,db)
}
func TestHealthToolsAvailableWithoutRecipeSage(t *testing.T) {
 s:=healthServer(t)
 resp:=invoke(t,s,"tools/list","{}")
 body,err:=json.Marshal(resp.Result)
 if err!=nil{t.Fatal(err)}
 for _,name:=range []string{"health_record_add","health_plan_save","health_workout_pending","health_vo2_estimate","health_nutrition_log"} {
  if !strings.Contains(string(body),name){t.Errorf("tool %s not registered",name)}
 }
 if strings.Contains(string(body),"recipe_delete"){t.Fatal("unconfigured RecipeSage tools exposed")}
}
func TestHealthRecordCall(t *testing.T) {
 s:=healthServer(t)
 p:=`{"name":"health_record_add","arguments":{"domain":"biometric","metric":"weight","value":75.2,"unit":"kg","method":"measured","source":"scale"}}`
 resp:=invoke(t,s,"tools/call",p)
 b,_:=json.Marshal(resp.Result)
 if strings.Contains(string(b),`"isError":true`){t.Fatal(string(b))}
 listed:=invoke(t,s,"tools/call",`{"name":"health_record_list","arguments":{"domain":"biometric","metric":"weight"}}`)
 result,_:=json.Marshal(listed.Result)
 if !strings.Contains(string(result),"75.2"){t.Fatalf("missing observation: %s",result)}
}
func TestMissingWorkoutCannotBeAutoMissed(t *testing.T) {
 s:=healthServer(t)
 plan:=`{"name":"health_plan_save","arguments":{"kind":"workout","title":"Cardio","expected_revision":0,"sessions":[{"id":"run","date":"2020-01-01","title":"Run"}]}}`
 response:=invoke(t,s,"tools/call",plan)
 b,_:=json.Marshal(response.Result)
 if strings.Contains(string(b),`"isError":true`){t.Fatal(string(b))}
 var inner struct{Content []struct{Text string `json:"text"`} `json:"content"`}
 if err:=json.Unmarshal(b,&inner);err!=nil{t.Fatal(err)}
 var saved health.Plan
 if err:=json.Unmarshal([]byte(inner.Content[0].Text),&saved);err!=nil{t.Fatal(err)}
 args,_:=json.Marshal(map[string]any{"name":"health_workout_pending","arguments":map[string]any{"plan_id":saved.ID,"as_of":time.Now().UTC().Format(time.RFC3339)}})
 pending:=invoke(t,s,"tools/call",string(args))
 value,_:=json.Marshal(pending.Result)
 if !strings.Contains(string(value),"unconfirmed"){t.Fatalf("pending classification incorrect: %s",value)}
 if strings.Contains(string(value),"missed"){t.Fatal("workout automatically classified missed")}
}
