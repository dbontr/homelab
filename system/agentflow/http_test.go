package agentflow

import (
 "context"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestEngineeringUnixSocketLifecycle(t *testing.T) {
 engine,_:=testEngine(t)
 socket:=filepath.Join(t.TempDir(),"engineering.sock")
 if err:=os.Chmod(filepath.Dir(socket),0700);err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background())
 terminated:=make(chan error,1)
 go func(){terminated<-ServeUnix(ctx,socket,engine)}()
 defer cancel()
 client,err:=NewClient(socket)
 if err!=nil{t.Fatal(err)}
 deadline:=time.Now().Add(3*time.Second)
 for {
  if _,err:=client.List();err==nil{break}
  if time.Now().After(deadline){
   select{case err:=<-terminated:t.Fatalf("engineering service failed: %v",err);default:}
   t.Fatal("engineering service did not become available")
  }
  time.Sleep(10*time.Millisecond)
 }
 info,err:=os.Stat(socket)
 if err!=nil{t.Fatal(err)}
 if info.Mode().Perm()!=0660{t.Fatalf("unexpected socket permissions: %o",info.Mode().Perm())}
 created,err:=client.Submit(sampleProgram())
 if err!=nil{t.Fatal(err)}
 if created.Status!=Pending||created.ID==""{t.Fatalf("unexpected submission: %+v",created)}
 runs,err:=client.List()
 if err!=nil||len(runs)!=1||runs[0].ID!=created.ID{t.Fatalf("invalid run list: %+v %v",runs,err)}
 found,err:=client.Get(created.ID)
 if err!=nil||found.Revision!=created.Revision{t.Fatalf("invalid run lookup: %+v %v",found,err)}
 if err:=client.Cancel(created.ID,0);err==nil{t.Fatal("stale cancellation accepted")}
 if err:=client.Cancel(created.ID,created.Revision);err!=nil{t.Fatal(err)}
 cancelled,err:=client.Get(created.ID)
 if err!=nil||!cancelled.Cancelled||cancelled.Status!=Cancelled{t.Fatalf("cancellation not visible: %+v %v",cancelled,err)}
 cancel()
 select{
 case err:=<-terminated:if err!=nil{t.Fatalf("engineering socket shutdown failed: %v",err)}
 case <-time.After(3*time.Second):t.Fatal("engineering socket did not terminate")
 }
}
