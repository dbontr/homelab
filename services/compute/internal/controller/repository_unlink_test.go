package controller

import (
 "strings"
 "testing"
)

func TestUnlinkRepositoryExactDoesNotAffectOtherRepositoriesOrHistory(t *testing.T){
 db,_,srv:=fixture(t)
 project(t,srv)
 if _,err:=db.LinkRepositoryWithRole("demo","dbontr/stale","source","main");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkRepositoryWithRole("demo","dbontr/active","source","main");err!=nil{t.Fatal(err)}
 if _,err:=db.CreateProject("other","Other");err!=nil{t.Fatal(err)}
 if _,err:=db.LinkRepositoryWithRole("other","dbontr/stale","research","main");err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{name,repo,role,ref string}{
  {"role conflict","dbontr/stale","planning","main"},
  {"ref conflict","dbontr/stale","source","develop"},
  {"missing","dbontr/missing","source","main"},
  {"path traversal","../invalid","source","main"},
 } {
  t.Run(tc.name,func(t *testing.T){
   if _,err:=db.UnlinkRepositoryExact("demo",tc.repo,tc.role,tc.ref);err==nil{t.Fatal("unsafe unlink accepted")}
   if !db.RepositoryLinked("demo","dbontr/stale"){t.Fatal("rejected unlink removed valid repository")}
  })
 }
 out,err:=db.UnlinkRepositoryExact("demo","dbontr/stale","source","main")
 if err!=nil||out["status"]!="unlinked"{t.Fatalf("expected exact unlink: %v %v",out,err)}
 if db.RepositoryLinked("demo","dbontr/stale"){t.Fatal("obsolete repository remained linked")}
 if !db.RepositoryLinked("demo","dbontr/active")||!db.RepositoryLinked("other","dbontr/stale"){
  t.Fatal("unrelated repository links changed")
 }
 if _,err:=db.UnlinkRepositoryExact("demo","dbontr/stale","source","main");err==nil||!strings.Contains(err.Error(),"not found"){
  t.Fatal("unlink replay was not rejected")
 }
}
