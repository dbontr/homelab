package agentflow

import (
 "strings"
 "testing"
)
func TestGoSemanticsResolvesTypedLocalDefinitions(t *testing.T){
 source:=\`package sample
type Position struct{ X int }
func shift(value Position) Position { value.X++; return value }
func run() { _ = shift(Position{X:3}) }
\`
 result,err:=analyzeGoSource("core/sample.go",source)
 if err!=nil{t.Fatal(err)}
 if result.Partial||len(result.Diagnostics)!=0{t.Fatalf("valid Go type analysis partial: %+v",result)}
 found:=false
 for _,symbol:=range result.Symbols {
  if symbol.Name=="shift"&&symbol.Line==4&&symbol.DefinitionLine==3&&strings.Contains(symbol.Type,"func("){
   found=true
  }
 }
 if !found{t.Fatalf("typed reference was not linked to definition: %+v",result.Symbols)}
}
func TestGoSemanticsNeverCertifiesUnknownImports(t *testing.T){
 result,err:=analyzeGoSource("core/test.go",\`package sample
import unknown "example.invalid/private"
func run() { unknown.Execute() }\`)
 if err!=nil{t.Fatal(err)}
 if !result.Partial||len(result.Diagnostics)==0{t.Fatal("unresolved dependencies must produce partial report")}
 for _,name:=range []string{"../private.go",".env","secrets.json"}{
  if _,err:=analyzeGoSource(name,"package sample");err==nil{t.Fatalf("protected path accepted: %s",name)}
 }
}

func TestGoSemanticPackageResolvesCrossFileDefinitions(t *testing.T){
 code:=map[string]string{
  "core/types.go":`package core
type Element struct { Value int }
func increment(e Element) Element { e.Value++; return e }`,
  "core/runner.go":`package core
func run() Element {
 return increment(Element{Value: 7})
}`,
 }
 result,err:=analyzeGoPackage("core/runner.go",code)
 if err!=nil{t.Fatal(err)}
 if result.Partial{t.Fatalf("cross-file type checking failed: %+v",result)}
 found:=false
 for _,symbol:=range result.Symbols{
  if symbol.Name=="increment"&&symbol.DefinitionFile=="types.go"&&symbol.DefinitionLine==3{
   found=true
  }
 }
 if !found{t.Fatalf("cross-file symbol definition missing: %+v",result)}
 code["core/outsider.go"]="package other"
 result,err=analyzeGoPackage("core/runner.go",code)
 if err!=nil||!result.Partial{t.Fatal("incomplete Go package reported as complete")}
}
