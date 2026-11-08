package transport

import (
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
 for _,kind:=range []string{"task","goal","recipe","meal","book"}{
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
 resource,err:=server.resource(kind,id)
 if err!=nil||resource.Owner!="personal"||resource.Reference!=resourceRef("task",task.ID){
  t.Fatalf("resource diverged from source record: %+v %v",resource,err)
 }
}
