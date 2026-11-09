package release

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

func TestFleetIPCForwardsOwnerAssertionWithoutPrivilegedKeys(t *testing.T){
 s,_:=testFleetHTTP(t)
 dir:=t.TempDir();if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 socket:=filepath.Join(dir,"fleet.sock")
 ctx,cancel:=context.WithCancel(context.Background())
 done:=make(chan error,1)
 go func(){done<-s.ServeSocket(ctx,socket,1000)}()
 defer func(){cancel();if err:=<-done;err!=nil{t.Error(err)}}()
 for i:=0;i<100;i++{
  if _,err:=os.Lstat(socket);err==nil{break}
  time.Sleep(10*time.Millisecond)
 }
 client,err:=NewFleetClient(socket);if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{}`)
 if _,err:=client.Call(context.Background(),"infrastructure__fleet_status",args);err==nil{
  t.Fatal("unsigned gateway request reached protected endpoint")
 }
 raw,err:=client.Call(security.WithAccessAssertion(context.Background(),"signed-owner"),"infrastructure__fleet_status",args)
 if err!=nil||!strings.Contains(string(raw),"confirmation required"){
  t.Fatal("signed identity was not used for authorization",string(raw),err)
 }
 if _,err:=client.Call(context.Background(),"infrastructure__fleet_unrestricted_shell",args);err==nil{
  t.Fatal("unknown fleet operation permitted")
 }
}
