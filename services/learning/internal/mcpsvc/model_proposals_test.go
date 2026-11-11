package mcpsvc

import (
    "context"
    "encoding/json"
    "errors"
    "math"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/learning/internal/store"
    "github.com/dbontr/homelab-new/system/learning/internal/teacheval"
)

type proposalModel struct {
    reply string
    calls int
    messages []teacheval.Message
    err error
}

func (m *proposalModel) Complete(_ context.Context, _ string, messages []teacheval.Message) (string,error) {
    m.calls++
    m.messages=messages
    if m.err!=nil{return "",m.err}
    return m.reply,nil
}

func TestProvisionalQuestionAssessmentKeepsCommittedAnswerSealed(t *testing.T) {
    svc:=testService(t)
    _,err:=svc.Store.AddConcept("mathematics-limit","Limits","One common epsilon argument","mathematics")
    if err!=nil{t.Fatal(err)}
    session,err:=svc.Store.StartLearningSession("Understand limits","mathematics-limit","learn",nil)
    if err!=nil{t.Fatal(err)}
    question,err:=svc.Store.OpenLearningQuestion(session["id"].(string),"mathematics-limit","",
        "diagnostic","Which quantifier is uniform?","multiple_choice",
        []store.QuestionOption{{ID:"a",Label:"Separate index"},{ID:"b",Label:"One index"}},
        "b","Quantifiers must be justified")
    if err!=nil{t.Fatal(err)}
    id:=question["id"].(string)
    _,err=svc.Store.AnswerLearningQuestionWithReflection(id,"b",false,
        "The index is common to all points.",nil)
    if err!=nil{t.Fatal(err)}
    model:=&proposalModel{reply:`{"verdict":"supported","evaluation":"The selected option matches the committed answer.","confidence":0.8,"reasoning_verdict":"supported","reasoning_evaluation":"The explanation identifies the shared index."}`}
    if err:=svc.SetModelProvider(model,"reviewer-model");err!=nil{t.Fatal(err)}
    proposed,err:=svc.proposeQuestionAssessment(context.Background(),id)
    if err!=nil{t.Fatal(err)}
    if proposed["model_proposal"]!=true||proposed["mastery_awarded"]!=false||
        proposed["requires_review"]!=true||proposed["verdict"]!="supported" {
        t.Fatalf("model proposal gained assessment authority: %+v",proposed)
    }
    // The sealed key may be disclosed only to the configured evaluator,
    // never through the learner's public question record.
    var input map[string]any
    if err:=json.Unmarshal([]byte(model.messages[1].Content),&input);err!=nil{t.Fatal(err)}
    if input["expected_answer"]!="b"{t.Fatal("model was not given the precommitted key")}
    public,err:=svc.Store.GetLearningQuestion(id)
    if err!=nil{t.Fatal(err)}
    if public["status"]!="answered"{t.Fatal("model proposal changed grading state")}
    if _,leak:=public["expected_answer"];leak{t.Fatal("model proposal leaked answer key")}
    model.reply=`{"verdict":"gap","evaluation":"Invented mismatch","confidence":0.9}`
    if _,err:=svc.proposeQuestionAssessment(context.Background(),id);err==nil{
        t.Fatal("model was allowed to contradict the precommitted correct choice")
    }
    model.reply=`{"verdict":"supported","evaluation":"looks good","confidence":0.7,"reasoning_verdict":"supported"}`
    if _,err:=svc.proposeQuestionAssessment(context.Background(),id);err==nil{
        t.Fatal("reasoning verdict lacked a documented evaluation")
    }
}

func TestModelProposalsRejectMalformedAndNonFiniteResults(t *testing.T) {
    svc:=testService(t)
    var proposal ModelAssessment
    if err:=svc.modelJSON(context.Background(),"policy",map[string]any{},&proposal);err==nil{
        t.Fatal("model was implicitly enabled")
    }
    model:=&proposalModel{}
    if err:=svc.SetModelProvider(model,"model");err!=nil{t.Fatal(err)}
    if err:=svc.SetModelProvider(model,"other");err==nil{
        t.Fatal("model provider was changed after configuration")
    }
    for _,reply:=range []string{
        `{"verdict":"supported","evaluation":"x","confidence":0.5,"unknown":1}`,
        `{"verdict":"supported","evaluation":"x","confidence":0.5} {}`,
        `{}`+strings.Repeat(" ",25<<10),
    }{
        model.reply=reply
        if err:=svc.modelJSON(context.Background(),"policy",map[string]any{},&proposal);err==nil{
            t.Fatalf("malformed model reply accepted: %q",reply[:min(len(reply),120)])
        }
    }
    for _,confidence:=range []float64{-0.1,1.1,math.NaN(),math.Inf(1)} {
        if validAssessment(ModelAssessment{Verdict:"supported",Evaluation:"valid",Confidence:confidence},false)==nil{
            t.Fatalf("invalid model confidence admitted: %v",confidence)
        }
    }
    model.err=errors.New("provider unavailable")
    if err:=svc.modelJSON(context.Background(),"policy",nil,&proposal);err==nil{
        t.Fatal("provider failure reported as a completed assessment")
    }
}

func TestStagedModelExtractionRequiresIndependentAudit(t *testing.T) {
    svc:=testService(t)
    source,err:=svc.Store.IngestText("Lecture excerpt","notes","text/plain",
        "A derivative measures the local rate of change.")
    if err!=nil{t.Fatal(err)}
    model:=&proposalModel{reply:`{"claims":[{"statement":"A derivative measures a local rate of change","context":"calculus","domain":"mathematics","quote":"A derivative measures the local rate of change."}],"chains":[]}`}
    if err:=svc.SetModelProvider(model,"source-extractor");err!=nil{t.Fatal(err)}
    result,err:=svc.stageSourceExtraction(context.Background(),source["id"].(string))
    if err!=nil{t.Fatal(err)}
    if result["status"]!="pending_audit" || result["requires_review"]!=true ||
        result["mastery_awarded"]!=false {
        t.Fatalf("model extraction bypassed separate review: %+v",result)
    }
    model.reply=`{"claims":[{"statement":"A derivative is an integral","context":"calculus","domain":"mathematics","quote":"No such quote"}],"chains":[]}`
    // An existing batch is returned for audit; it must not be regenerated
    // from a second, changed proposal.
    next,err:=svc.stageSourceExtraction(context.Background(),source["id"].(string))
    if err!=nil || next["action"]!="audit" {
        t.Fatalf("pending audit was replaced by another extraction: %+v %v",next,err)
    }
}
