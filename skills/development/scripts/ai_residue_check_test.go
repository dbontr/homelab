package main

import(
 "os"
 "path/filepath"
 "testing"
)

func TestDetectsAssistantPathAndAttribution(t *testing.T){
 dir:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(dir,"CLAUDE.md"),[]byte("config"),0600);err!=nil{t.Fatal(err)}
 claim:="generated "+"by AI"
 if err:=os.WriteFile(filepath.Join(dir,"note.txt"),[]byte("This document was "+claim+"."),0600);err!=nil{t.Fatal(err)}
 items,err:=check(dir,nil)
 if err!=nil{t.Fatal(err)}
 found:=map[string]bool{}
 for _,item:=range items{found[item.Code]=true}
 if !found["AI_FILE"]||!found["AI_ATTRIBUTION"]{t.Fatalf("missing findings: %+v",items)}
}

func TestExplicitAllowAndOrdinaryFiles(t *testing.T){
 dir:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(dir,"AGENTS.md"),[]byte("config"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"README.md"),[]byte("# Project\n"),0600);err!=nil{t.Fatal(err)}
 found,err:=check(dir,[]string{"AGENTS.md"})
 if err!=nil||len(found)!=0{t.Fatalf("allowlist ignored: %+v %v",found,err)}
 if pathFinding("src/.claude/settings.json")==nil{t.Fatal("assistant directory allowed")}
}
