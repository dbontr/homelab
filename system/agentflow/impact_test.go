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
func TestImpactCrossLanguageDependenciesAndMissingSource(t *testing.T) {
    source:=CodeMap{Files:[]CodeOutline{
        {Path:"src/math/vector.rs",Language:"rust"},
        {Path:"src/math/vector_test.rs",Language:"rust"},
        {Path:"src/compute/runner.rs",Language:"rust",Imports:[]string{"crate::math::vector::Vector"}},
        {Path:"src/compute/runner_tests.rs",Language:"rust"},
        {Path:"src/unrelated/example_test.rs",Language:"rust"},
    }}
    impact,err:=testImpactFromMap(source,"src/math/vector.rs")
    if err!=nil{t.Fatal(err)}
    if impact.Incomplete||len(impact.CandidateTests)!=2||
       impact.CandidateTests[0].Path!="src/compute/runner_tests.rs"||
       impact.CandidateTests[1].Path!="src/math/vector_test.rs"{t.Fatalf("missing Rust test dependencies: %+v",impact)}
    generated,err:=testImpactFromMap(CodeMap{Files:source.Files[:1]},"src/compute/runner.rs")
    if err!=nil||!generated.Incomplete{t.Fatal("missing changed source was represented as complete")}
    generated2,err:=testImpactFromMap(CodeMap{Files:[]CodeOutline{{Path:"src/compute/runner.rs",Language:"rust",Partial:true}}},"src/compute/runner.rs")
    if err!=nil||!generated2.Incomplete{t.Fatal("partial source outline was treated as complete")}
    module,err:=outlineSource("src/lib.rs","mod math;\nuse crate::math::vector::Vector;\n")
    if err!=nil||len(module.Imports)!=2||module.Imports[0]!="self::math" {t.Fatalf("module imports lost: %+v %v",module,err)}
}
func TestImpactReportsCompilerDiagnosticsWithoutClaimingVerifiedTests(t *testing.T) {
    report:=TestImpact{Changed:"src/main.go"}
    attachGoImpact(&report,GoSemanticReport{Partial:true,Diagnostics:[]string{"unresolved package import"}})
    if !report.Incomplete||!report.GoAnalysisPartial||len(report.GoDiagnostics)!=1 {t.Fatalf("missing partial compiler report: %+v",report)}
    if !importedSource(CodeOutline{Path:"src/solver.c",Language:"c-family",Imports:[]string{"types.h"}},"src/types.h") {t.Fatal("C header include not linked")}
    if !importedSource(CodeOutline{Path:"src/test.vhd",Language:"vhdl",Imports:[]string{"work.fifo.all"}},"src/fifo.vhd") {t.Fatal("VHDL package reference not linked")}
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
