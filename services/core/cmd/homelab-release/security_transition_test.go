package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
 "errors"
)

func TestSecurityUnitSelectionRequiresProtectedReceipt(t *testing.T){
 cases:=[]struct{name,content,implementation,receipt string;valid bool}{
  {"Go", "[Service]\nExecStart=/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-homelab-security/bin/homelab-securityd\nLoadCredential=password:/vault/password totp:/vault/totp migration-receipt:/srv/homelab/secrets/security/authority-go-cutover.json\n", "go","/srv/homelab/secrets/security/authority-go-cutover.json",true},
  {"Rust", "[Service]\nExecStart=/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-rust-security/bin/securityd\nLoadCredential=migration-receipt:/srv/homelab/secrets/security/authority-rust-cutover.json\n","rust","/srv/homelab/secrets/security/authority-rust-cutover.json",true},
  {"missing receipt","[Service]\nExecStart=/nix/store/a/bin/homelab-securityd\n","", "",false},
  {"duplicate receipts","[Service]\nExecStart=/nix/store/a/bin/homelab-securityd\nLoadCredential=migration-receipt:/a migration-receipt:/b\n","","",false},
  {"unexpected executable","[Service]\nExecStart=/tmp/securityd\nLoadCredential=migration-receipt:/vault/receipt\n","","",false},
  {"relative credential","[Service]\nExecStart=/nix/store/a/bin/securityd\nLoadCredential=migration-receipt:relative\n","","",false},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   impl,receipt,err:=parseSecurityUnit([]byte(tc.content))
   if tc.valid {
    if err!=nil||impl!=tc.implementation||receipt!=tc.receipt {
     t.Fatalf("wrong selection: %q %q %v",impl,receipt,err)
    }
   }else if err==nil{t.Fatalf("unsafe security configuration accepted: %s, %s",impl,receipt)}
  })
 }
}

func TestTransitionRequiresExactStaticOperatorEnvironment(t *testing.T){
 t.Setenv("HOMELAB_RELEASE_SECURITY_TRANSITION","")
 if transition,err:=securityTransitionFromEnv();err!=nil||transition!=nil {
  t.Fatalf("disabled transition unexpectedly started: %v",err)
 }
 // Test a deliberately incomplete operator environment even when the\n // caller itself carries a fully configured production release context.\n t.Setenv("HOMELAB_RELEASE_SECURITY_STATE","")\n t.Setenv("HOMELAB_RELEASE_SECURITY_TRANSITION","1")\n if _,err:=securityTransitionFromEnv();err==nil {
  t.Fatal("incomplete protected credential configuration was accepted")
 }
 t.Setenv("HOMELAB_RELEASE_SECURITY_TRANSITION","wrong")
 if _,err:=securityTransitionFromEnv();err==nil {
  t.Fatal("unrecognized transition mode was accepted")
 }
}

func TestProtectedTransitionReceiptRejectsUntrustedCustody(t *testing.T){
 dir:=t.TempDir()
 if err:=os.WriteFile(dir+"/unsafe",[]byte("{}"),0644);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(dir+"/unsafe",0644);err!=nil{t.Fatal(err)}
 if _,err:=transitionFile(dir+"/unsafe",1024);err==nil{
  t.Fatal("world-readable protected credential admitted")
 }
 if err:=os.Symlink("unsafe",dir+"/link");err!=nil{t.Fatal(err)}
 if _,err:=transitionFile(dir+"/link",1024);err==nil{
  t.Fatal("symlink credential admitted")
 }
}

func TestSecurityUnitRejectsMisleadingExecutableSuffix(t *testing.T){
 for _,suffix:=range []string{"securityd --skip-migration","homelab-securityd --skip-migration","bad-securityd"}{
  text:="[Service]\nExecStart=/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-security/bin/"+suffix+"\nLoadCredential=migration-receipt:/srv/homelab/secrets/security/authority-go-cutover.json\n"
  if _,_,err:=parseSecurityUnit([]byte(text));err==nil {
   t.Fatalf("unrecognized security executable accepted: %s",strings.TrimSpace(suffix))
  }
 }
}

func TestFailedMigrationRestartsStillSelectedSecurityAuthority(t *testing.T){
 failed:=errors.New("current-state migration rejected")
 calls:=[]string{}
 err:=transitionCaptureWithRecovery(
  func()error{calls=append(calls,"stop");return nil},
  func()error{calls=append(calls,"capture");return failed},
  func()error{calls=append(calls,"restore");return nil},
 )
 if !errors.Is(err,failed)||strings.Join(calls,",")!="stop,capture,restore"{
  t.Fatalf("failed capture did not restore old authority: %v, calls %v",err,calls)
 }
}

func TestFailedAuthorityStopRestoresWithoutAttemptingCapture(t *testing.T){
 stopFailure:=errors.New("service refused to stop")
 calls:=[]string{}
 err:=transitionCaptureWithRecovery(
  func()error{calls=append(calls,"stop");return stopFailure},
  func()error{calls=append(calls,"capture");return nil},
  func()error{calls=append(calls,"restore");return nil},
 )
 if !errors.Is(err,stopFailure)||strings.Join(calls,",")!="stop,restore"{
  t.Fatalf("failed stop altered authority custody: %v, %v",err,calls)
 }
}

func TestFailedRecoveryRetainsBothErrors(t *testing.T){
 captureFailure:=errors.New("migration receipt unavailable")
 restoreFailure:=errors.New("prior authority failed health check")
 err:=transitionCaptureWithRecovery(
  func()error{return nil},
  func()error{return captureFailure},
  func()error{return restoreFailure},
 )
 if !errors.Is(err,captureFailure)||!errors.Is(err,restoreFailure){
  t.Fatalf("recovery lost the failure evidence: %v",err)
 }
}

func TestSuccessfulAuthorityHandoffDoesNotRestartPreviousService(t *testing.T){
 called:=false
 err:=transitionCaptureWithRecovery(
  func()error{return nil},
  func()error{return nil},
  func()error{called=true;return nil},
 )
 if err!=nil||called{t.Fatalf("successful migration restarted retired authority: %v",err)}
}


func TestResolveNixExecutableSymlinkBeforeIndependentPreflight(t *testing.T){
 root:=t.TempDir()
 binary:=filepath.Join(root,"homelab-security-preflight")
 link:=filepath.Join(root,"generation-preflight")
 if err:=os.WriteFile(binary,[]byte("#!/bin/sh\nexit 0\n"),0500);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(binary,0500);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(binary,link);err!=nil{t.Fatal(err)}
 resolved,err:=resolveRegularExecutable(link)
 if err!=nil||resolved!=binary {t.Fatalf("immutable Nix symlink was not resolved: %q %v",resolved,err)}
 if err:=os.Chmod(binary,0777);err!=nil{t.Fatal(err)}
 if _,err:=resolveRegularExecutable(link);err==nil{t.Fatal("writable preflight executable accepted")}
 if _,err:=resolveRegularExecutable(filepath.Join(root,"missing"));err==nil{t.Fatal("missing preflight accepted")}
}
