package agentflow

import (
    "context"
    "encoding/json"
    "errors"
    "path"
    "sort"
    "strings"
)

type TestImpactCandidate struct {
    Path string `json:"path"`
    Reason string `json:"reason"`
}
type TestImpact struct {
    Changed string `json:"changed"`
    AffectedDirectories []string `json:"affected_directories"`
    CandidateTests []TestImpactCandidate `json:"candidate_tests"`
    Incomplete bool `json:"incomplete"`
    Basis string `json:"basis"`
    GoAnalysisPartial bool `json:"go_analysis_partial,omitempty"`
    GoDiagnostics []string `json:"go_diagnostics,omitempty"`
}

func testSourceFile(name string) bool {
    basename:=path.Base(name)
    return strings.HasSuffix(basename,"_test.go")||
        strings.HasSuffix(basename,"_test.rs")||strings.HasSuffix(basename,"_tests.rs")||
        strings.HasSuffix(basename,"_test.cpp")||strings.HasSuffix(basename,"_test.c")||
        strings.HasPrefix(basename,"test_")&&strings.HasSuffix(basename,".py")||
        strings.HasSuffix(basename,"_test.py")||
        strings.HasSuffix(basename,".test.ts")||strings.HasSuffix(basename,".spec.ts")||
        strings.HasSuffix(basename,".test.tsx")||strings.HasSuffix(basename,".spec.tsx")||
        strings.HasSuffix(basename,".test.js")||strings.HasSuffix(basename,".spec.js")||
        strings.Contains("/"+name,"/tests/")
}

func importTargetsDirectory(importPath,dir string) bool {
    if dir==""||dir=="."||strings.HasPrefix(importPath,".") {return false}
    return importPath==dir||strings.HasSuffix(importPath,"/"+dir)
}

// The map can be truncated. In particular, absent imports or tests never
// establish that the changed file is safe or that no tests are needed.
func testImpactFromMap(index CodeMap,changed string)(TestImpact,error) {
    if !validSourcePath(changed)||sensitiveSourcePath(changed)||sourceLanguage(changed)=="" {
        return TestImpact{},errors.New("invalid impact source")
    }
    affected:=map[string]bool{path.Dir(changed):true}
    incomplete:=index.Truncated
    foundChange:=false
    for _,file:=range index.Files {
        if file.Path==changed{foundChange=true}
        if file.Partial||file.Truncated{incomplete=true}
    }
    if !foundChange{incomplete=true}
    // Conservative fixed-point walk across packages visible in the bounded map.
    for i:=0;i<=len(index.Files);i++ {
        grew:=false
        for _,file:=range index.Files {
            directory:=path.Dir(file.Path)
            if affected[directory]||!validSourcePath(file.Path)||sensitiveSourcePath(file.Path){continue}
            dependent:=false
            for _,imported:=range file.Imports {
                if file.Language!="go"{continue}
                for dependency:=range affected {
                    if importTargetsDirectory(imported,dependency){dependent=true;break}
                }
                if dependent{break}
            }
            if !dependent {
                for _,candidate:=range index.Files {
                    if affected[path.Dir(candidate.Path)]&&file.Path!=candidate.Path&&
                       importedSource(file,candidate.Path){dependent=true;break}
                }
            }
            if dependent{affected[directory]=true;grew=true}
        }
        if !grew{break}
    }
    report:=TestImpact{
        Changed:changed,Incomplete:incomplete,
        AffectedDirectories:make([]string,0,len(affected)),
        CandidateTests:[]TestImpactCandidate{},
        Basis:"heuristic: bounded source outlines and declared imports; not validated test coverage",
    }
    for directory:=range affected {report.AffectedDirectories=append(report.AffectedDirectories,directory)}
    sort.Strings(report.AffectedDirectories)
    for _,file:=range index.Files {
        if !validSourcePath(file.Path)||sensitiveSourcePath(file.Path)||
            !affected[path.Dir(file.Path)]||!testSourceFile(file.Path){continue}
        report.CandidateTests=append(report.CandidateTests,TestImpactCandidate{
            Path:file.Path,Reason:"test file in the changed or a visible importing directory",
        })
    }
    sort.Slice(report.CandidateTests,func(i,j int)bool {
        return report.CandidateTests[i].Path<report.CandidateTests[j].Path
    })
    if len(report.CandidateTests)>20 {
        report.CandidateTests=report.CandidateTests[:20]
        report.Incomplete=true
    }
    return report,nil
}

func (e *Engine) repositoryTestImpact(ctx context.Context,w Workflow,brief AgentBrief,changed string)(string,error) {
    indexed,err:=e.repositoryMap(ctx,w,brief,"")
    if err!=nil{return "",err}
    var source CodeMap
    if err:=json.Unmarshal([]byte(indexed),&source);err!=nil{return "",errors.New("invalid repository source map")}
    // A global enumeration can be truncated before the changed file.
    // Include a second map of the source directory without claiming completeness.
    directory:=path.Dir(changed)
    if directory!="." {
        raw,err:=e.repositoryMap(ctx,w,brief,directory)
        if err!=nil{return "",err}
        var local CodeMap
        if err:=json.Unmarshal([]byte(raw),&local);err!=nil{return "",errors.New("invalid directory source map")}
        seen:=map[string]bool{}
        for _,file:=range source.Files{seen[file.Path]=true}
        for _,file:=range local.Files{if !seen[file.Path]{source.Files=append(source.Files,file);seen[file.Path]=true}}
        source.Truncated=source.Truncated||local.Truncated
    }
    report,err:=testImpactFromMap(source,changed)
    if err!=nil{return "",err}
    if sourceLanguage(changed)=="go" {
        raw,err:=e.repositoryGoSemantics(ctx,w,brief,changed)
        if err!=nil{report.Incomplete=true;report.GoAnalysisPartial=true
        }else{
            var evidence GoSemanticReport
            if err:=json.Unmarshal([]byte(raw),&evidence);err!=nil{report.Incomplete=true;report.GoAnalysisPartial=true
            }else{attachGoImpact(&report,evidence)}
        }
    }
    encoded,err:=json.Marshal(report)
    if err!=nil{return "",err}
    if len(encoded)>12<<10{return "",errors.New("impact observation exceeds limit")}
    return string(encoded),nil
}
