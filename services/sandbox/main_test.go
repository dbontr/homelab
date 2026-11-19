package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestSandboxIdentifiersAndCommandsStayBounded(t *testing.T){
 if !hexID(strings.Repeat("a",32),32)||!hexID(strings.Repeat("f",40),40,64)||
  !hexID(strings.Repeat("3",64),40,64){t.Fatal("valid signed identifiers rejected")}
 for _,value:=range []string{"../bad","AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",strings.Repeat("a",31),
  strings.Repeat("q",40),strings.Repeat("b",65)}{
  if hexID(value,32,40,64){t.Fatalf("unsafe identifier: %q",value)}
 }
 for _,value:=range []string{"owner","repository","foo-bar","project_1"}{
  if !validRepoName(value){t.Errorf("valid repository component rejected %q",value)}
 }
 for _,value:=range []string{"",".","..",".git","repo.","a/b","../secrets","x\n"}{
  if validRepoName(value){t.Fatalf("unsafe repository component accepted %q",value)}
 }
 for _,action:=range []string{"go-test","go-vet","nix-check"}{
  if _,err:=localAction(action);err!=nil{t.Fatalf("approved check denied: %s",action)}
 }
 for _,action:=range []string{"go-test","go-vet"}{
  if _,err:=containedAction(action);err!=nil{t.Fatalf("approved contained check denied: %s",action)}
 }
 for _,action:=range []string{"sh","git-push","nix-activate","kubectl","rm","go-run"}{
  if _,err:=localAction(action);err==nil{t.Fatalf("unsafe local action accepted: %s",action)}
  if _,err:=containedAction(action);err==nil{t.Fatalf("unsafe contained action accepted: %s",action)}
 }
}

func TestSourceImportReceiptMustBindExactOwnerAndRevision(t *testing.T){
 dir:=t.TempDir()
 receipt:=filepath.Join(dir,".pwm-source-revision")
 revision:=strings.Repeat("a",40)
 valid:="example/research\n"+revision+"\n"
 if err:=os.WriteFile(receipt,[]byte(valid),0600);err!=nil{t.Fatal(err)}
 if err:=verifyReceipt(dir,"example/research",revision);err!=nil{t.Fatal(err)}
 for _,bad:=range []string{"example/other\n"+revision+"\n",
  "example/research\n"+strings.Repeat("b",40)+"\n",
  valid+"extra\n"}{
  if err:=os.WriteFile(receipt,[]byte(bad),0600);err!=nil{t.Fatal(err)}
  if err:=verifyReceipt(dir,"example/research",revision);err==nil{
   t.Fatal("mismatched immutable import receipt was accepted")
  }
 }
 if err:=os.Remove(receipt);err!=nil{t.Fatal(err)}
 destination:=filepath.Join(dir,"redirected")
 if err:=os.WriteFile(destination,[]byte(valid),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(destination,receipt);err!=nil{t.Fatal(err)}
 if err:=verifyReceipt(dir,"example/research",revision);err==nil{
  t.Fatal("symbolic-link source import receipt was accepted")
 }
}

func TestNonRootBoundariesRefuseUntrustedModes(t *testing.T){
 if err:=run([]string{"run","../bad",strings.Repeat("a",40),"go-test"});err==nil{
  t.Fatal("untrusted workspace was accepted")
 }
 if err:=run([]string{"run-contained","owner/../repo",strings.Repeat("a",40),"go-test"});err==nil{
  t.Fatal("untrusted repository was accepted")
 }
 if err:=run([]string{"run-contained","owner/repo","not-a-sha","go-test"});err==nil{
  t.Fatal("unverified revision was accepted")
 }
 if err:=run([]string{"run-contained","owner/repo",strings.Repeat("a",40),"nix-activate"});err==nil{
  t.Fatal("unapproved contained action was accepted")
 }
}
