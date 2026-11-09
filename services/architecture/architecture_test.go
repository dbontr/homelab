package architecture

import (
 "go/parser"
 "go/token"
 "io/fs"
 "os"
 "path/filepath"
 "runtime"
 "strconv"
 "strings"
 "testing"
)

func repositoryRoot(t *testing.T) string {
 t.Helper()
 _, filename, _, ok := runtime.Caller(0)
 if !ok { t.Fatal("source path unavailable") }
 return filepath.Clean(filepath.Join(filepath.Dir(filename), "../.."))
}

func TestServicesHaveIndependentSourceOwnership(t *testing.T) {
 root := repositoryRoot(t)
 modules := map[string]string{
  "services/core":     "github.com/dbontr/homelab-new/system",
  "services/compute":  "github.com/dbontr/homelab-new/system/compute",
  "services/learning": "github.com/dbontr/homelab-new/system/learning",
  "services/health":   "github.com/dbontr/homelab-new/system/health",
 }
 for directory, module := range modules {
  file := filepath.Join(root, directory, "go.mod")
  contents, err := os.ReadFile(file)
  if err != nil { t.Fatalf("%s: %v",directory,err) }
  if !strings.HasPrefix(string(contents), "module "+module+"\n") {
   t.Errorf("%s changed Go module identity; this requires explicit import and release migration",directory)
  }
 }
 for _, stale := range []string{"system/go.mod","system/compute/go.mod","system/learning/go.mod","system/health/go.mod"} {
  _, err := os.Stat(filepath.Join(root,stale))
  if !os.IsNotExist(err) { t.Errorf("superseded module source remains at %s",stale) }
 }
 workspace,err:=os.ReadFile(filepath.Join(root,"go.work"))
 if err!=nil{t.Fatal(err)}
 for directory:=range modules{
  if !strings.Contains(string(workspace),"./"+directory){
   t.Errorf("go.work omits %s",directory)
  }
 }
}

func TestInterfaceImplementationsDoNotOwnServiceLogic(t *testing.T) {
 root:=repositoryRoot(t)
 targets:=[]string{"services","api","mcp"}
 for _,dir:=range targets {
  source:=filepath.Join(root,dir)
  err:=filepath.WalkDir(source,func(path string,entry fs.DirEntry,walkErr error)error{
   if walkErr!=nil{return walkErr}
   if entry.IsDir()||!strings.HasSuffix(path,".go"){return nil}
   file,err:=parser.ParseFile(token.NewFileSet(),path,nil,parser.ImportsOnly)
   if err!=nil{return err}
   for _,importSpec:=range file.Imports{
    imported,err:=strconv.Unquote(importSpec.Path.Value)
    if err!=nil{return err}
    if strings.HasPrefix(path,filepath.Join(root,"services")+string(filepath.Separator)){
     for _,forbidden:=range []string{
      "github.com/dbontr/homelab-new/api",
      "github.com/dbontr/homelab-new/mcp",
      "github.com/dbontr/homelab-new/ui",
     } {
      if imported==forbidden||strings.HasPrefix(imported,forbidden+"/"){
       t.Errorf("%s: service cannot import an optional interface %s",path,imported)
      }
     }
     if !strings.HasPrefix(path,filepath.Join(root,"services/core")+string(filepath.Separator))&&
       (imported=="github.com/dbontr/homelab-new/system/transport"||
        strings.HasPrefix(imported,"github.com/dbontr/homelab-new/system/transport/")){
      t.Errorf("%s: a specialist service cannot depend on the central transport",path)
     }
    }else if strings.HasPrefix(imported,"github.com/dbontr/homelab-new/system/"){
     t.Errorf("%s: optional interface cannot import core application logic %s",path,imported)
    }
   }
   return nil
  })
  if err!=nil{t.Fatal(err)}
 }
}
