package profiles

import (
 "errors"
 "os"
 "path/filepath"
 "testing"
)

func TestVersionedBuildProfilePersistence(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"profiles.json")
 r,err:=Open(path);if err!=nil{t.Fatal(err)}
 spec:=Spec{ID:"pynq-builder",Title:"AUP-ZU3 PYNQ builder",Environment:"vivado-2025",SoftwareIDs:[]string{"petalinux"},Repository:"dbontr/aup-zu3-pynq",Revision:"commit-id",Parameters:map[string]string{"RAM_GB":"8"},Steps:[]Step{{ID:"build",Command:"make board=aup-zu3"}},Outputs:[]string{"build/pynq.img"},Requires:Requirements{Architecture:"amd64",CPU:8,MemoryMB:16384,Hardware:map[string]int{"fpga":1}}}
 one,err:=r.Save(spec,0);if err!=nil||one.Version!=1{t.Fatalf("save revision 1: %v, %+v",err,one)}
 spec.Parameters["RAM_GB"]="4"
 two,err:=r.Save(spec,1);if err!=nil||two.Version!=2{t.Fatalf("save revision 2: %v, %+v",err,two)}
 if _,err=r.Save(spec,1);err==nil{t.Fatal("accepted outdated revision")}
 one,err=r.Get("pynq-builder",1);if err!=nil||one.Parameters["RAM_GB"]!="8"{t.Fatal("old revision changed")}
 reopened,err:=Open(path);if err!=nil{t.Fatal(err)}
 latest,err:=reopened.Get("pynq-builder",0);if err!=nil||latest.Parameters["RAM_GB"]!="4"{t.Fatal("profile not durable")}
 latest.Parameters["RAM_GB"]="corrupted"
 if reopened.List()[0].Parameters["RAM_GB"]!="4"{t.Fatal("list returned mutable state")}
 if _,err=reopened.Get("nonexistent",0);!errors.Is(err,os.ErrNotExist){t.Fatal("missing profile not reported")}
 stat,err:=os.Stat(path);if err!=nil||stat.Mode().Perm()!=0600{t.Fatal("storage not owner-only")}
}

func TestInvalidBuildProfilesAreRejected(t *testing.T) {
 r,err:=Open(filepath.Join(t.TempDir(),"profiles.json"));if err!=nil{t.Fatal(err)}
 base:=Spec{ID:"test",Title:"Test",Steps:[]Step{{ID:"build",Command:"make"}}}
 for _,change:=range []func(*Spec){
   func(s *Spec){s.ID="bad/name"},
   func(s *Spec){s.Steps[0].Command=""},
   func(s *Spec){s.Steps[0].WorkingDir="../secrets"},
   func(s *Spec){s.Outputs=[]string{"/etc/passwd"}},
   func(s *Spec){s.Requires.Hardware=map[string]int{"fpga":-1}},
   func(s *Spec){s.Parameters=map[string]string{"API_TOKEN":"do-not-store"}},
 }{
   copy:=base;copy.Steps=append([]Step(nil),base.Steps...);change(&copy)
   if _,err:=r.Save(copy,0);err==nil{t.Fatalf("invalid profile accepted: %+v",copy)}
 }
}
