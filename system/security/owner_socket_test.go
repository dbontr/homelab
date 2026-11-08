package security

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
)

func TestOwnerUnlockGrantsOnlyAllowedOperation(t *testing.T){
 record,err:=HashPassphrase([]byte("private-local-operator-passphrase"));if err!=nil{t.Fatal(err)}
 unlock,err:=NewOwnerUnlock(record);if err!=nil{t.Fatal(err)}
 auth,err:=NewAuthority([]byte(strings.Repeat("s",32)),filepath.Join(t.TempDir(),"spent.json"));if err!=nil{t.Fatal(err)}
 owner,err:=NewOwnerApprovals(auth,unlock,[]string{"cluster.configure"});if err!=nil{t.Fatal(err)}
 send:=func(pass,op string)*httptest.ResponseRecorder{
  encoded,_:=json.Marshal(map[string]any{"passphrase":pass,"operation":op,"target":"cluster","arguments":map[string]any{"change":"test"}})
  req:=httptest.NewRequest("POST","/approvals/issue",bytes.NewReader(encoded))
  out:=httptest.NewRecorder()
  owner.ownerHandler(out,req)
  return out
 }
 if out:=send("private-local-operator-passphrase","cluster.delete");out.Code!=403{t.Fatal("unknown handler was approved")}
 if out:=send("incorrect-passphrase","cluster.configure");out.Code!=401{t.Fatal("bad passphrase approved")}
 out:=send("private-local-operator-passphrase","cluster.configure")
 if out.Code!=200{t.Fatalf("owner approval rejected: %s",out.Body.String())}
 var result struct{Grant string `json:"grant"`}
 if err:=json.Unmarshal(out.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
 if result.Grant==""{t.Fatal("grant missing")}
 args:=json.RawMessage(`{"change":"test"}`)
 if err:=auth.Consume(result.Grant,"cluster.configure","cluster",args);err!=nil{t.Fatal(err)}
 if err:=auth.Consume(result.Grant,"cluster.configure","cluster",args);err==nil{t.Fatal("replayed grant accepted")}
 if _,err:=NewOwnerApprovals(auth,nil,[]string{"test"});err==nil{t.Fatal("missing verifier accepted")}
}

func TestOwnerSocketRequiresPrivateDirectory(t *testing.T){
 record,err:=HashPassphrase([]byte("private-local-operator-passphrase"));if err!=nil{t.Fatal(err)}
 unlock,err:=NewOwnerUnlock(record);if err!=nil{t.Fatal(err)}
 auth,err:=NewAuthority([]byte(strings.Repeat("s",32)),filepath.Join(t.TempDir(),"spent.json"));if err!=nil{t.Fatal(err)}
 owner,err:=NewOwnerApprovals(auth,unlock,[]string{"cluster.configure"});if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=owner.ServeOwnerSocket(ctx,"relative.sock");err==nil{t.Fatal("accepted relative owner socket")}
}
