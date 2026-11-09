package agentflow

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "go/ast"
 "go/importer"
 "go/parser"
 "go/token"
 "go/types"
 "path"
 "sort"
)

type GoReference struct {
 Name string `json:"name"`
 File string `json:"file"`
 Line int `json:"line"`
 Column int `json:"column"`
 DefinitionLine int `json:"definition_line,omitempty"`
 Type string `json:"type,omitempty"`
}
type GoSemanticReport struct {
 Path string `json:"path"`
 Package string `json:"package"`
 Symbols []GoReference `json:"symbols"`
 Diagnostics []string `json:"diagnostics"`
 Partial bool `json:"partial"`
 Truncated bool `json:"truncated"`
}

// analyzeGoSource uses Go's type checker, not lexical token matching. A
// standalone file cannot resolve every repository dependency, so unresolved
// imports result in an explicit partial report and no success claim.
func analyzeGoSource(filename,source string)(GoSemanticReport,error){
 report:=GoSemanticReport{Path:filename,Symbols:[]GoReference{},Diagnostics:[]string{}}
 if !validSourcePath(filename)||sensitiveSourcePath(filename)||sourceLanguage(filename)!="go"||
  len(source)>maxCodeFileSize{return report,errors.New("invalid or oversized Go source")}
 fileset:=token.NewFileSet()
 parsed,err:=parser.ParseFile(fileset,filename,source,parser.AllErrors|parser.ParseComments)
 if parsed==nil{return report,errors.New("cannot parse Go source")}
 report.Package=parsed.Name.Name
 if err!=nil{
  report.Partial=true
  report.Diagnostics=append(report.Diagnostics,err.Error())
 }
 uses:=map[*ast.Ident]types.Object{}
 defs:=map[*ast.Ident]types.Object{}
 info:=&types.Info{Defs:defs,Uses:uses,Types:map[ast.Expr]types.TypeAndValue{}}
 config:=types.Config{
  Importer:importer.Default(),
  Error:func(err error){
   report.Partial=true
   if len(report.Diagnostics)<12{report.Diagnostics=append(report.Diagnostics,err.Error())}else{report.Truncated=true}
  },
 }
 _,typeErr:=config.Check(report.Package,fileset,[]*ast.File{parsed},info)
 if typeErr!=nil{report.Partial=true}
 add:=func(identifier *ast.Ident,obj types.Object){
  if obj==nil||identifier==nil||identifier.Name=="_"||len(report.Symbols)>=80{
   if len(report.Symbols)>=80{report.Truncated=true}
   return
  }
  position:=fileset.Position(identifier.Pos())
  if !position.IsValid(){return}
  entry:=GoReference{Name:identifier.Name,File:path.Base(filename),Line:position.Line,Column:position.Column}
  if typ:=obj.Type();typ!=nil{
   entry.Type=types.TypeString(typ,func(p *types.Package)string{return p.Name()})
   if len(entry.Type)>180{entry.Type=entry.Type[:180];report.Truncated=true}
  }
  definition:=fileset.Position(obj.Pos())
  if definition.IsValid()&&definition.Filename==filename{entry.DefinitionLine=definition.Line}
  report.Symbols=append(report.Symbols,entry)
 }
 ast.Inspect(parsed,func(node ast.Node)bool{
  ident,ok:=node.(*ast.Ident)
  if !ok{return true}
  if obj,found:=defs[ident];found{add(ident,obj)}else if obj,found:=uses[ident];found{add(ident,obj)}
  return true
 })
 sort.Slice(report.Symbols,func(i,j int)bool{
  if report.Symbols[i].Line!=report.Symbols[j].Line{return report.Symbols[i].Line<report.Symbols[j].Line}
  return report.Symbols[i].Column<report.Symbols[j].Column
 })
 return report,nil
}

func (e *Engine) repositoryGoSemantics(ctx context.Context,w Workflow,brief AgentBrief,filename string)(string,error){
 if !validSourcePath(filename)||sensitiveSourcePath(filename)||sourceLanguage(filename)!="go"{
  return "",errors.New("invalid semantic source")
 }
 root:=repoRoot(brief.Repository)
 reply,err:=e.workspaceAction(ctx,w,"read_file",map[string]any{"path":path.Join(root,filename)})
 if err!=nil{return "",err}
 result,err:=analyzeGoSource(filename,str(reply["content"]))
 if err!=nil{return "",err}
 encoded,err:=json.Marshal(result)
 if err!=nil{return "",err}
 if len(encoded)>16<<10{return "",fmt.Errorf("Go semantic report exceeds observation limit")}
 return string(encoded),nil
}
