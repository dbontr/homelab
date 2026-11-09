package transport

import (
 "context"
 "encoding/json"
 "path/filepath"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestCanonicalResourceReferencesRejectSubstitution(t *testing.T){
 cases:=[]string{
  "", "homelab:task:../../etc/passwd","homelab:task:", "homelab:admin:abc",
  "homelab:task:abc:extra","homelab:task:abc%2Fdef","workspace:task:abc",
 }
 for _,input:=range cases{
  if _,_,err:=parseResourceReference(input);err==nil{t.Fatalf("invalid reference accepted: %q",input)}
 }
 for _,kind:=range []string{"task","goal","recipe","meal","book","project"}{
  ref:=resourceRef(kind,"abc_123")
  got,id,err:=parseResourceReference(ref)
  if err!=nil||got!=kind||id!="abc_123"{t.Fatalf("round trip failed: %s %v",ref,err)}
 }
}

func TestResourceReferenceResolvesOwningStore(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"personal.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 task,err:=db.AddTask(store.TaskInput{Title:"Review paper"})
 if err!=nil{t.Fatal(err)}
 server:=&Server{db:db}
 kind,id,err:=parseResourceReference(resourceRef("task",task.ID))
 if err!=nil{t.Fatal(err)}
 resource,err:=server.resource(context.Background(),kind,id)
 if err!=nil||resource.Owner!="personal"||resource.Reference!=resourceRef("task",task.ID){
  t.Fatalf("resource diverged from source record: %+v %v",resource,err)
 }
 raw,_:=json.Marshal(map[string]string{"kind":"task","id":task.ID})
 value,err:=server.localTool(context.Background(),"resource_get",raw)
 if err!=nil{t.Fatal(err)}
 resolved,ok:=value.(Resource)
 if !ok||resolved.Reference!=resource.Reference{t.Fatalf("MCP resource resolution mismatch: %#v",value)}
}
