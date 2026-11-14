package main

import(
 "os"
 "path/filepath"
 "testing"
)

func TestReferencesContract(t *testing.T){
 dir:=t.TempDir()
 file:=filepath.Join(dir,"REFERENCES.md")
 valid:="# References\n\n- [Go](https://go.dev/doc/) — Go team. Used for: Go language specification.\n"
 if err:=os.WriteFile(file,[]byte(valid),0600);err!=nil{t.Fatal(err)}
 if got:=check(dir);len(got)!=0{t.Fatalf("valid references rejected: %+v",got)}
 if err:=os.WriteFile(file,[]byte(valid+
  "- [Same](https://GO.DEV/doc) — Go team. Used for: duplicated URL.\n"),0600);err!=nil{t.Fatal(err)}
 items:=check(dir)
 found:=false
 for _,item:=range items{if item.Code=="DUPLICATE"{found=true}}
 if !found{t.Fatal("duplicate canonical URLs were not detected")}
}

func TestMissingPageAndInvalidSources(t *testing.T){
 dir:=t.TempDir()
 if got:=check(dir);len(got)!=1||got[0].Code!="MISSING_REFERENCES"{
  t.Fatal("missing ledger was not detected")
 }
 data:="# Incorrect\n\n- [Chat](https://chatgpt.com/share/abc) — Chat. Used for: a conversation.\n"+
  "- [Local](file:///tmp/source) — Local. Used for: invalid source.\n"
 if err:=os.WriteFile(filepath.Join(dir,"REFERENCES.md"),[]byte(data),0600);err!=nil{t.Fatal(err)}
 found:=map[string]bool{}
 for _,item:=range check(dir){found[item.Code]=true}
 for _,code:=range []string{"HEADING","EPHEMERAL_AI","LOCAL_PATH"}{
  if !found[code]{t.Fatalf("reference error %s was not detected",code)}
 }
}
