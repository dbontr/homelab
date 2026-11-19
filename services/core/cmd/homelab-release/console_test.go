package main

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestConsoleSessionRequiresLocalRootSeat(t *testing.T){
 correct:=map[string]string{
  "User":"0","Seat":"seat0","Type":"tty","Remote":"no",
  "Service":"login","TTY":"tty1",
 }
 if !validConsoleSession(0,"/dev/tty1","127",correct){t.Fatal("local root console denied")}
 if validConsoleSession(1000,"/dev/tty1","127",correct){t.Fatal("nonroot user approved")}
 if validConsoleSession(0,"/dev/pts/1","127",correct){t.Fatal("pseudoterminal approved")}
 if validConsoleSession(0,"/dev/tty1","session",correct){t.Fatal("invalid session approved")}
 for key,value:=range map[string]string{
  "User":"1000","Seat":"","Type":"x11","Remote":"yes",
  "Service":"sshd","TTY":"pts/1",
 } {
  props:=map[string]string{}
  for k,v:=range correct{props[k]=v}
  props[key]=value
  if validConsoleSession(0,"/dev/tty1","127",props){t.Fatalf("untrusted %s approved",key)}
 }
}

func TestConsoleGuardianActivatesAndRejectsReplay(t *testing.T){
 ctrl,current,stageID:=guardianFixture(t)
 requestID:=strings.Repeat("6",32)
 intent:=consoleReleaseIntent{
  RequestID:requestID,ReleaseID:stageID,Host:"saturn",
  Authority:"local-root-console",TTY:"/dev/tty1",SessionID:"127",
  Created:time.Now().UTC(),
 }
 if err:=consoleWriteRecord(ctrl.Config.StateDir,"intent",requestID,intent);err!=nil{t.Fatal(err)}
 if err:=consoleGuardActivate(context.Background(),ctrl,requestID,stageID);err!=nil{t.Fatal(err)}
 status,err:=ctrl.Status()
 if err!=nil||status.Phase!="active"||*current!=guardianTarget{
  t.Fatalf("console release not active: %+v %v",status,err)
 }
 if err:=consoleGuardActivate(context.Background(),ctrl,requestID,stageID);err==nil{
  t.Fatal("console request replay accepted")
 }
 if _,err:=os.Stat(consoleRecordPath(ctrl.Config.StateDir,"claimed",requestID));err!=nil{
  t.Fatal("durable claim missing")
 }
 if _,err:=os.Stat(consoleRecordPath(ctrl.Config.StateDir,"result",requestID));err!=nil{
  t.Fatal("durable outcome missing")
 }
}

func TestConsoleGuardianRejectsWrongStageAndHost(t *testing.T){
 ctrl,current,stageID:=guardianFixture(t)
 requestID:=strings.Repeat("7",32)
 intent:=consoleReleaseIntent{
  RequestID:requestID,ReleaseID:stageID,Host:"jupiter",
  Authority:"local-root-console",TTY:"/dev/tty1",SessionID:"127",
  Created:time.Now().UTC(),
 }
 if err:=consoleWriteRecord(ctrl.Config.StateDir,"intent",requestID,intent);err!=nil{t.Fatal(err)}
 if err:=consoleGuardActivate(context.Background(),ctrl,requestID,stageID);err==nil{
  t.Fatal("wrong target host approved")
 }
 if *current!=guardianOld{t.Fatal("incorrect console operation changed system")}
 if _,err:=os.Stat(consoleRecordPath(ctrl.Config.StateDir,"claimed",requestID));!os.IsNotExist(err){
  t.Fatal("rejected console operation was claimed")
 }
}

func TestConsoleReceiptRejectsExpiryAndUnsafePermissions(t *testing.T){
 ctrl,_,stageID:=guardianFixture(t)
 requestID:=strings.Repeat("8",32)
 intent:=consoleReleaseIntent{
  RequestID:requestID,ReleaseID:stageID,Host:"saturn",
  Authority:"local-root-console",TTY:"/dev/tty1",SessionID:"127",
  Created:time.Now().Add(-16*time.Minute).UTC(),
 }
 if err:=consoleWriteRecord(ctrl.Config.StateDir,"intent",requestID,intent);err!=nil{t.Fatal(err)}
 if _,err:=consoleReadIntent(ctrl.Config.StateDir,requestID);err==nil{t.Fatal("expired console request approved")}
 path:=consoleRecordPath(ctrl.Config.StateDir,"intent",requestID)
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=consoleReadIntent(ctrl.Config.StateDir,requestID);err==nil{t.Fatal("world-readable console record approved")}
 if err:=os.Remove(path);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(filepath.Join(ctrl.Config.StateDir,"journal.json"),path);err!=nil{t.Fatal(err)}
 if _,err:=consoleReadIntent(ctrl.Config.StateDir,requestID);err==nil{t.Fatal("symlink console record approved")}
}

func TestConsoleReceiptRejectsUntrustedDirectory(t *testing.T){
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0777);err!=nil{t.Fatal(err)}
 if err:=consoleWriteRecord(dir,"intent",strings.Repeat("c",32),map[string]string{"a":"b"});err==nil{
  t.Fatal("untrusted state directory received console authorization")
 }
}
