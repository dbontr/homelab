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
    DefinitionFile string `json:"definition_file,omitempty"`
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

// A compiler type-check operates on all available files from the same
// package. Missing files or imports are reported as partial rather than
// mistakenly presented as a complete compiler validation.
func analyzeGoPackage(filename string,sources map[string]string)(GoSemanticReport,error){
    report:=GoSemanticReport{Path:filename,Symbols:[]GoReference{},Diagnostics:[]string{}}
    if !validSourcePath(filename)||sensitiveSourcePath(filename)||sourceLanguage(filename)!="go"||
        len(sources)==0||len(sources)>12{return report,errors.New("invalid bounded Go package")}
    if _,ok:=sources[filename];!ok{return report,errors.New("target source is absent")}
    paths:=make([]string,0,len(sources))
    for name:=range sources{paths=append(paths,name)}
    sort.Strings(paths)
    fileset:=token.NewFileSet()
    var files []*ast.File
    total:=0
    for _,name:=range paths{
        body:=sources[name]
        if !validSourcePath(name)||sensitiveSourcePath(name)||sourceLanguage(name)!="go"||
            path.Dir(name)!=path.Dir(filename)||len(body)>maxCodeFileSize{
            return report,errors.New("invalid Go package source")
        }
        total+=len(body)
        if total>256<<10{return report,errors.New("Go package exceeds analysis limit")}
        parsed,err:=parser.ParseFile(fileset,name,body,parser.AllErrors)
        if parsed==nil{
            if name==filename{return report,errors.New("cannot parse target source")}
            report.Partial=true
            if len(report.Diagnostics)<12{report.Diagnostics=append(report.Diagnostics,"package source cannot be parsed: "+name)}
            continue
        }
        if name==filename{report.Package=parsed.Name.Name}
        if err!=nil{
            report.Partial=true
            if len(report.Diagnostics)<12{report.Diagnostics=append(report.Diagnostics,err.Error())}
        }
        files=append(files,parsed)
    }
    if report.Package==""{return report,errors.New("Go package has no target file")}
    pkgFiles:=make([]*ast.File,0,len(files))
    for _,file:=range files{
        if file.Name.Name==report.Package{pkgFiles=append(pkgFiles,file)}else{
            report.Partial=true
            if len(report.Diagnostics)<12{report.Diagnostics=append(report.Diagnostics,"other Go package excluded")}
        }
    }
    definitions:=map[*ast.Ident]types.Object{}
    usages:=map[*ast.Ident]types.Object{}
    info:=&types.Info{Defs:definitions,Uses:usages,Types:map[ast.Expr]types.TypeAndValue{}}
    config:=types.Config{Importer:importer.Default(),Error:func(err error){
        report.Partial=true
        if len(report.Diagnostics)<12{report.Diagnostics=append(report.Diagnostics,err.Error())}else{report.Truncated=true}
    }}
    _,err:=config.Check(report.Package,fileset,pkgFiles,info)
    if err!=nil{report.Partial=true}
    for _,file:=range pkgFiles{
        ast.Inspect(file,func(node ast.Node)bool{
            identifier,ok:=node.(*ast.Ident)
            if !ok||identifier.Name=="_"{return true}
            position:=fileset.Position(identifier.Pos())
            if position.Filename!=filename{return true}
            var object types.Object
            if defined,found:=definitions[identifier];found{object=defined}else{object=usages[identifier]}
            if object==nil{return true}
            if len(report.Symbols)>=80{report.Truncated=true;return false}
            entry:=GoReference{
                Name:identifier.Name,File:path.Base(filename),
                Line:position.Line,Column:position.Column,
            }
            if typ:=object.Type();typ!=nil{
                entry.Type=types.TypeString(typ,func(pkg *types.Package)string{
                    if pkg==nil{return ""}
                    return pkg.Name()
                })
                if len(entry.Type)>180{entry.Type=entry.Type[:180];report.Truncated=true}
            }
            definedAt:=fileset.Position(object.Pos())
            if definedAt.IsValid(){
                for _,name:=range paths{
                    if definedAt.Filename==name{
                        entry.DefinitionLine=definedAt.Line
                        entry.DefinitionFile=path.Base(name)
                        break
                    }
                }
            }
            report.Symbols=append(report.Symbols,entry)
            return true
        })
    }
    sort.Slice(report.Symbols,func(i,j int)bool{
        if report.Symbols[i].Line!=report.Symbols[j].Line{return report.Symbols[i].Line<report.Symbols[j].Line}
        return report.Symbols[i].Column<report.Symbols[j].Column
    })
    return report,nil
}

func analyzeGoSource(filename,source string)(GoSemanticReport,error){
    return analyzeGoPackage(filename,map[string]string{filename:source})
}

func (e *Engine) repositoryGoSemantics(ctx context.Context,w Workflow,brief AgentBrief,filename string)(string,error){
    if !validSourcePath(filename)||sensitiveSourcePath(filename)||sourceLanguage(filename)!="go"{
        return "",errors.New("invalid Go analysis path")
    }
    root:=repoRoot(brief.Repository)
    get:=func(name string)(string,error){
        reply,err:=e.workspaceAction(ctx,w,"read_file",map[string]any{"path":path.Join(root,name)})
        if err!=nil{return "",err}
        value,ok:=reply["content"].(string)
        if !ok||len(value)>maxCodeFileSize{return "",errors.New("source file exceeds analysis limit")}
        return value,nil
    }
    target,err:=get(filename)
    if err!=nil{return "",err}
    sources:=map[string]string{filename:target}
    raw,err:=e.repositoryMap(ctx,w,brief,path.Dir(filename))
    if err!=nil{return "",err}
    var mapped CodeMap
    if err:=json.Unmarshal([]byte(raw),&mapped);err!=nil{return "",err}
    incomplete:=mapped.Truncated
    for _,entry:=range mapped.Files{
        if len(sources)>=12{incomplete=true;break}
        if entry.Path==filename||path.Dir(entry.Path)!=path.Dir(filename)||
           entry.Language!="go"||!validSourcePath(entry.Path)||sensitiveSourcePath(entry.Path){continue}
        body,err:=get(entry.Path)
        if err!=nil{incomplete=true;continue}
        sources[entry.Path]=body
    }
    report,err:=analyzeGoPackage(filename,sources)
    if err!=nil{return "",err}
    if incomplete{report.Partial=true;report.Truncated=true}
    encoded,err:=json.Marshal(report)
    if err!=nil{return "",err}
    if len(encoded)>16<<10{return "",fmt.Errorf("Go analysis exceeds observation limit")}
    return string(encoded),nil
}
