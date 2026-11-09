package agentflow

import (
    "strings"
    "testing"
)

func TestImpactReportsTestsInChangedAndImportingPackages(t *testing.T) {
    source:=CodeMap{Scope:"",Files:[]CodeOutline{
        {Path:"src/math/math.go",Language:"go"},
        {Path:"src/math/math_test.go",Language:"go"},
        {Path:"src/compute/runner.go",Language:"go",Imports:[]string{"github.com/example/project/src/math"}},
        {Path:"src/compute/runner_test.go",Language:"go"},
        {Path:"src/other/unrelated_test.go",Language:"go"},
    }}
    got,err:=testImpactFromMap(source,"src/math/math.go")
    if err!=nil{t.Fatal(err)}
    if got.Incomplete||len(got.CandidateTests)!=2||got.CandidateTests[0].Path!="src/compute/runner_test.go"||
        got.CandidateTests[1].Path!="src/math/math_test.go"{
        t.Fatalf("unexpected conservative impact: %+v",got)
    }
    if len(got.AffectedDirectories)!=2||got.AffectedDirectories[0]!="src/compute" {
        t.Fatalf("dependent package missing: %+v",got)
    }
    source.Truncated=true
    got,err=testImpactFromMap(source,"src/math/math.go")
    if err!=nil||!got.Incomplete{t.Fatal("truncated repository map presented as complete")}
}
func TestImpactCannotExpandAgentWriteAuthority(t *testing.T) {
    brief:=AgentBrief{Repository:"example/primary",AllowedPaths:[]string{"src/math/math.go"},
        LinkedRepositories:[]RepositoryPin{{Repository:"example/secondary",Revision:strings.Repeat("a",40)}}}
    for _,decision:=range []AgentAction{
        {Action:"test_impact",Path:"src/math/math.go"},
        {Action:"test_impact",Repository:"example/secondary",Path:"src/math/math.go"},
    } {
        if err:=decision.validate(brief);err!=nil{t.Fatal(err)}
    }
    for _,decision:=range []AgentAction{
        {Action:"test_impact",Path:"../../etc/passwd"},
        {Action:"test_impact",Path:".env"},
        {Action:"patch",Repository:"example/secondary",Path:"src/math/math.go"},
    } {
        if err:=decision.validate(brief);err==nil{t.Fatalf("unauthorized action accepted: %+v",decision)}
    }
}
