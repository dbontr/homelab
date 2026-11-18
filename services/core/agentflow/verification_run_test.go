package agentflow

import (
    "context"
    "strings"
    "testing"
    "time"
)

func verificationProgram() Program {
    p := sampleProgram()
    p.VerifyOnly = true
    p.PrepareOnly = true
    p.Changes = nil
    p.AllowedPaths = nil
    p.ExpectedHead = ""
    p.BaseBranch = ""
    p.Title = ""
    p.Objective = "Verify the pinned source using its declared compiler environment"
    return p
}

func TestVerificationOnlyRunHasNoAuthoringOrPublication(t *testing.T) {
    e, compute := testEngine(t)
    program := verificationProgram()
    run, err := e.Submit(program)
    if err != nil {
        t.Fatal(err)
    }
    for attempt := 0; attempt < 4; attempt++ {
        current, err := e.Get(run.ID)
        if err != nil {
            t.Fatal(err)
        }
        if current.Status == Succeeded {
            break
        }
        progress, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).
            RunOne(context.Background(), run.ID)
        if err != nil || !progress {
            t.Fatalf("verification step %d: progress=%v, error=%v", attempt, progress, err)
        }
    }
    final, err := e.Get(run.ID)
    if err != nil || final.Status != Succeeded {
        t.Fatalf("verification was not confirmed: %+v %v", final, err)
    }
    if len(final.Steps) != 3 || compute.drafts != 0 || compute.writes != 0 {
        t.Fatalf("verification modified or published source: steps=%+v drafts=%d writes=%d",
            final.Steps, compute.drafts, compute.writes)
    }
    for _, step := range final.Steps {
        if step.Kind == "author" || step.Kind == "apply" || step.Kind == "export" ||
            step.Kind == "publish" {
            t.Fatalf("verification unexpectedly performed authoring: %s", step.Kind)
        }
    }
    if !strings.Contains(string(final.Steps[len(final.Steps)-1].Result),
        program.Revision) {
        t.Fatalf("verification omitted source revision: %+v", final.Steps)
    }
}

func TestVerificationOnlyRejectsMutationAndPublication(t *testing.T) {
    good := verificationProgram()
    for _, mutation := range []struct{
        title string
        edit func(*Program)
    }{
        {"publication", func(p *Program){p.PrepareOnly=false}},
        {"source edits", func(p *Program){p.Changes=map[string]string{"README.md":"new"}}},
        {"editable paths", func(p *Program){p.AllowedPaths=[]string{"README.md"}}},
        {"autonomous authoring", func(p *Program){p.Autonomous=true}},
        {"review", func(p *Program){p.Review=true}},
        {"knowledge side effect", func(p *Program){p.IncludeKnowledge=true}},
    } {
        t.Run(mutation.title, func(t *testing.T) {
            p := good
            mutation.edit(&p)
            if _, err := Compile(p); err == nil {
                t.Fatal("verification-only operation acquired a mutation path")
            }
        })
    }
    p := good
    p.VerifyOnly = false
    if _, err := Compile(p); err == nil {
        t.Fatal("normal authoring accepted missing source edits")
    }
}
