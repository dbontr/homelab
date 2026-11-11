package mcpsvc

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/dbontr/homelab-new/system/learning/internal/store"
    "github.com/dbontr/homelab-new/system/learning/internal/teacheval"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"strings"
)

const UIURI = "ui://adaptive-teaching/learning-workspace.html"

//go:embed ui.html
var appHTML string

type Service struct {
	Store  *store.Store
	Server *mcp.Server
    model teacheval.CompletionClient
    modelName string
    bundle *LearningAppBundle
}

func register[In any](server *mcp.Server, name, description string, handler func(In) (store.Value, error), app bool) {
	t := &mcp.Tool{Name: name, Description: description}
	if app {
		t.Meta = mcp.Meta{"ui": map[string]any{"resourceUri": UIURI, "visibility": []string{"model", "app"}}, "ui/resourceUri": UIURI}
	}
	mcp.AddTool(server, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		output, err := handler(in)
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(output)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, StructuredContent: output}, nil, nil
	})
}
func withView(view string, result store.Value) store.Value {
	if result == nil {
		result = store.Value{}
	}
	result["view"] = view
	return result
}
func NewService(db *store.Store) *Service {
	server := mcp.NewServer(&mcp.Implementation{Name: "Adaptive Teaching", Version: "1.0.0"}, nil)
    svc := &Service{Store:db,Server:server}
	server.AddResource(&mcp.Resource{URI: UIURI, Name: "Adaptive Teaching learning workspace", MIMEType: "text/html;profile=mcp-app"},
		func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
            html:=appHTML
            var meta mcp.Meta
            if svc.bundle!=nil {
                html=svc.bundle.resourceHTML()
                meta=svc.bundle.resourceMeta()
            }
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
                URI:UIURI,MIMEType:"text/html;profile=mcp-app",Text:html,Meta:meta,
            }}},nil
		})
	register(server, "show_knowledge_graph", "Open an interactive knowledge graph inside ChatGPT.", func(in struct {
		Focus string `json:"focus,omitempty"`
		Depth int    `json:"depth,omitempty"`
	}) (store.Value, error) {
		depth := in.Depth
		if depth == 0 {
			depth = 2
		}
		m, e := db.Graph(in.Focus, depth, 80)
		return withView("graph", m), e
	}, true)
	register(server, "show_practice_question", "Display one interactive practice question in ChatGPT.", func(in struct {
		ConceptID string `json:"concept_id"`
		Question  string `json:"question"`
		Dimension string `json:"dimension,omitempty"`
	}) (store.Value, error) {
		concept, e := db.Concept(in.ConceptID)
		if e != nil {
			return nil, e
		}
		dim := in.Dimension
		if dim == "" {
			dim = "justification"
		}
		return store.Value{"view": "question", "question": in.Question, "dimension": dim, "concept": store.Value{"id": in.ConceptID, "title": concept["title"]}, "mastery": concept["mastery"]}, nil
	}, true)
	register(server, "show_problem_workbench", "Continue an extended problem-solving attempt inside ChatGPT.", func(in struct {
		ProblemID string `json:"problem_id"`
	}) (store.Value, error) {
		m, e := db.GetProblem(in.ProblemID)
		return withView("problem", m), e
	}, true)
	register(server, "show_learning_session", "Inspect an adaptive teaching session and its reasoning route.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		m, e := db.GetLearningSession(in.SessionID)
		return withView("session", m), e
	}, true)

	register(server, "show_learning_question", "Open a committed diagnostic, prediction, or reasoning check in the learning workspace.", func(in struct {
		QuestionID string `json:"question_id"`
	}) (store.Value, error) {
		data, err := db.GetLearningQuestion(in.QuestionID)
		return withView("learning_question", data), err
	}, true)
	register(server, "open_learning_question", "Commit a question, correct answer, and grading criteria before the learner can answer it. One unresolved question per session.", func(in struct {
		SessionID       string                 `json:"session_id"`
		ConceptID       string                 `json:"concept_id"`
		StepID          string                 `json:"step_id,omitempty"`
		Kind            string                 `json:"kind"`
		Prompt          string                 `json:"prompt"`
		ResponseKind    string                 `json:"response_kind"`
		Options         []store.QuestionOption `json:"options,omitempty"`
		ExpectedAnswer  string                 `json:"expected_answer"`
		GradingCriteria string                 `json:"grading_criteria"`
	}) (store.Value, error) {
		return db.OpenLearningQuestion(in.SessionID, in.ConceptID, in.StepID, in.Kind, in.Prompt, in.ResponseKind, in.Options, in.ExpectedAnswer, in.GradingCriteria)
	}, false)
	register(server, "get_learning_question", "Read the pending or graded question; the answer key is sealed until grading is complete.", func(in struct {
		QuestionID string `json:"question_id"`
	}) (store.Value, error) {
		return db.GetLearningQuestion(in.QuestionID)
	}, false)
	register(server, "get_pending_learning_question", "Resume any unanswered or ungraded question in a learning session.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		return db.PendingLearningQuestion(in.SessionID)
	}, false)
	register(server, "answer_learning_question", "Save the learner's exact answer, explanation and optional self-confidence before grading. Self-confidence never establishes correctness.", func(in struct {
		QuestionID       string   `json:"question_id"`
		LearnerResponse  string   `json:"learner_response"`
		LearnerReasoning string   `json:"learner_reasoning,omitempty"`
		SelfConfidence   *float64 `json:"self_confidence,omitempty"`
		Assisted         bool     `json:"assisted,omitempty"`
	}) (store.Value, error) {
		return db.AnswerLearningQuestionWithReflection(in.QuestionID, in.LearnerResponse, in.Assisted, in.LearnerReasoning, in.SelfConfidence)
	}, false)
	register(server, "grade_learning_question", "Grade correctness against the committed key and separately assess the learner's reasoning. A correct choice without supported reasoning cannot validate a diagnostic prerequisite.", func(in struct {
		QuestionID          string  `json:"question_id"`
		Verdict             string  `json:"verdict"`
		Evaluation          string  `json:"evaluation"`
		Confidence          float64 `json:"confidence"`
		ReasoningVerdict    string  `json:"reasoning_verdict,omitempty"`
		ReasoningEvaluation string  `json:"reasoning_evaluation,omitempty"`
	}) (store.Value, error) {
		return db.GradeLearningQuestionWithReasoning(in.QuestionID, in.Verdict, in.Evaluation, in.Confidence, in.ReasoningVerdict, in.ReasoningEvaluation)
	}, false)

	register(server, "add_concept", "Record a curriculum concept without claiming that the learner knows it.", func(in struct {
		Identifier string `json:"identifier"`
		Title      string `json:"title"`
		Summary    string `json:"summary"`
		Domain     string `json:"domain,omitempty"`
	}) (store.Value, error) {
		return db.AddConcept(in.Identifier, in.Title, in.Summary, in.Domain)
	}, false)
	register(server, "connect_concepts", "Record a reasoned relation. 'requires' points from advanced concept to prerequisite.", func(in struct {
		Source    string `json:"source"`
		Target    string `json:"target"`
		Kind      string `json:"kind"`
		Rationale string `json:"rationale"`
	}) (store.Value, error) {
		return db.ConnectConcepts(in.Source, in.Target, in.Kind, in.Rationale)
	}, false)
	register(server, "add_justification", "Document why a claim holds; distinguish proof, evidence, axiom, definition, and model.", func(in struct {
		ConceptID       string   `json:"concept_id"`
		Claim           string   `json:"claim"`
		Kind            string   `json:"kind"`
		Premises        []string `json:"premises"`
		Explanation     string   `json:"explanation"`
		SourceReference string   `json:"source_reference,omitempty"`
	}) (store.Value, error) {
		return db.AddJustification(in.ConceptID, in.Claim, in.Kind, in.Premises, in.Explanation, in.SourceReference)
	}, false)
	register(server, "get_concept", "Read a concept, its explanations, prerequisites, and evidence-derived mastery.", func(in struct {
		Identifier string `json:"identifier"`
	}) (store.Value, error) {
		return db.Concept(in.Identifier)
	}, false)
	register(server, "record_assessment", "Record a real learner answer and explicit evaluation; do not infer mastery from model-authored answers.", func(in struct {
		ConceptID       string  `json:"concept_id"`
		Dimension       string  `json:"dimension"`
		Question        string  `json:"question"`
		LearnerResponse string  `json:"learner_response"`
		Evaluation      string  `json:"evaluation"`
		Passed          bool    `json:"passed"`
		Confidence      float64 `json:"confidence"`
		Assisted        bool    `json:"assisted,omitempty"`
	}) (store.Value, error) {
		return db.RecordEvidence(in.ConceptID, in.Dimension, in.Question, in.LearnerResponse, in.Evaluation, in.Passed, in.Confidence, in.Assisted)
	}, false)
	register(server, "set_learning_target", "Set mastery from 1 to 5 without altering prior evidence.", func(in struct {
		Level int `json:"level"`
	}) (store.Value, error) {
		return db.SetTarget(in.Level)
	}, false)
	register(server, "get_learning_summary", "Report concept counts, mastery levels, and the learning target.", func(in struct{}) (store.Value, error) { return db.Summary() }, false)
	register(server, "recommend_learning", "Find unverified concepts and prerequisite gaps.", func(in struct {
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 8
		}
		return db.Recommend(limit)
	}, false)
	register(server, "get_graph", "Read a bounded neighborhood of the learning graph.", func(in struct {
		Focus string `json:"focus,omitempty"`
		Depth int    `json:"depth,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		depth, limit := in.Depth, in.Limit
		if depth == 0 {
			depth = 2
		}
		if limit == 0 {
			limit = 80
		}
		return db.Graph(in.Focus, depth, limit)
	}, false)
	register(server, "start_learning_session", "Begin a persistent learning goal without awarding mastery.", func(in struct {
		Goal            string `json:"goal"`
		TargetConceptID string `json:"target_concept_id"`
		Mode            string `json:"mode,omitempty"`
		TargetLevel     *int   `json:"target_level,omitempty"`
	}) (store.Value, error) {
		mode := in.Mode
		if mode == "" {
			mode = "learn"
		}
		return db.StartLearningSession(in.Goal, in.TargetConceptID, mode, in.TargetLevel)
	}, false)
	register(server, "list_learning_sessions", "Find learning goals to resume, preserving earlier reasoning.", func(in struct {
		IncludeCompleted bool `json:"include_completed,omitempty"`
		Limit            int  `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 30
		}
		return db.ListLearningSessions(in.IncludeCompleted, limit)
	}, false)
	register(server, "get_learning_session", "Read the session's phase, diagnostic answers, reasoning path, and evidence.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		return db.GetLearningSession(in.SessionID)
	}, false)
	register(server, "advance_learning_session", "Advance to a validated phase using optional revision fencing.", func(in struct {
		SessionID        string `json:"session_id"`
		NextPhase        string `json:"next_phase"`
		ExpectedRevision *int   `json:"expected_revision,omitempty"`
	}) (store.Value, error) {
		return db.AdvanceLearningSession(in.SessionID, in.NextPhase, in.ExpectedRevision)
	}, false)
	register(server, "next_learning_action", "Read the next permitted learning interaction, pending question, reasoning gap, or reassessment without awarding mastery.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		return db.NextLearningAction(in.SessionID)
	}, false)
	register(server, "recommend_session_probe", "Identify the nearest uncertain prerequisite without generating the question.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		return db.RecommendSessionProbe(in.SessionID)
	}, false)
	register(server, "record_session_probe", "Record a diagnostic answer without granting durable mastery.", func(in struct {
		SessionID       string  `json:"session_id"`
		ConceptID       string  `json:"concept_id"`
		Question        string  `json:"question"`
		LearnerResponse string  `json:"learner_response"`
		Evaluation      string  `json:"evaluation"`
		Outcome         string  `json:"outcome"`
		Confidence      float64 `json:"confidence"`
		Assisted        bool    `json:"assisted,omitempty"`
	}) (store.Value, error) {
		return db.RecordSessionProbe(in.SessionID, in.ConceptID, in.Question, in.LearnerResponse, in.Evaluation, in.Outcome, in.Confidence, in.Assisted)
	}, false)
	register(server, "build_learning_plan", "Choose a short prerequisite route and retain evaluated steps across revisions.", func(in struct {
		SessionID               string            `json:"session_id"`
		ExpectedRevision        *int              `json:"expected_revision,omitempty"`
		PreferredJustifications map[string]string `json:"preferred_justifications,omitempty"`
	}) (store.Value, error) {
		return db.BuildLearningPlan(in.SessionID, in.ExpectedRevision, in.PreferredJustifications)
	}, false)
	register(server, "record_learning_step", "Assess one learner-authored inference independently of conceptual mastery.", func(in struct {
		SessionID       string  `json:"session_id"`
		StepID          string  `json:"step_id"`
		LearnerResponse string  `json:"learner_response"`
		Evaluation      string  `json:"evaluation"`
		Verdict         string  `json:"verdict"`
		Confidence      float64 `json:"confidence"`
		Assisted        bool    `json:"assisted,omitempty"`
	}) (store.Value, error) {
		return db.RecordLearningStep(in.SessionID, in.StepID, in.LearnerResponse, in.Evaluation, in.Verdict, in.Confidence, in.Assisted)
	}, false)
	register(server, "create_problem", "Create a durable exercise; generated problems require a private answer and rubric.", func(in struct {
		Title             string   `json:"title"`
		Statement         string   `json:"statement"`
		Kind              string   `json:"kind,omitempty"`
		Mode              string   `json:"mode,omitempty"`
		Difficulty        string   `json:"difficulty,omitempty"`
		ConceptIDs        []string `json:"concept_ids,omitempty"`
		Discipline        string   `json:"discipline,omitempty"`
		Origin            string   `json:"origin,omitempty"`
		ReferenceSolution string   `json:"reference_solution,omitempty"`
		GradingRubric     string   `json:"grading_rubric,omitempty"`
		SourceReference   string   `json:"source_reference,omitempty"`
	}) (store.Value, error) {
		kind, mode, difficulty, origin := in.Kind, in.Mode, in.Difficulty, in.Origin
		if kind == "" {
			kind = "proof"
		}
		if mode == "" {
			mode = "independent"
		}
		if difficulty == "" {
			difficulty = "challenging"
		}
		if origin == "" {
			origin = "generated"
		}
		return db.CreateProblem(in.Title, in.Statement, kind, mode, difficulty, in.ConceptIDs, in.Discipline, origin, in.ReferenceSolution, in.GradingRubric, in.SourceReference)
	}, false)
	register(server, "get_problem", "Read a saved problem with hints, reviews, and attempts, without its private answer key.", func(in struct {
		ProblemID string `json:"problem_id"`
	}) (store.Value, error) {
		return db.GetProblem(in.ProblemID)
	}, false)
	register(server, "list_problems", "List incomplete or completed problems; answer keys remain sealed.", func(in struct {
		Status string `json:"status,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		status := in.Status
		if status == "" {
			status = "open"
		}
		limit := in.Limit
		if limit == 0 {
			limit = 30
		}
		return db.ListProblems(status, limit)
	}, false)
	register(server, "save_problem_draft", "Save an immutable learner solution, optional reasoning explanation and learner-reported confidence without assigning a grade.", func(in struct {
		ProblemID        string   `json:"problem_id"`
		Draft            string   `json:"draft"`
		LearnerReasoning string   `json:"learner_reasoning,omitempty"`
		SelfConfidence   *float64 `json:"self_confidence,omitempty"`
	}) (store.Value, error) {
		return db.SaveProblemDraftWithReflection(in.ProblemID, in.Draft, in.LearnerReasoning, in.SelfConfidence)
	}, false)
	register(server, "record_problem_hint", "Record the exact level of assistance; exam mode forbids hints.", func(in struct {
		ProblemID string `json:"problem_id"`
		Level     int    `json:"level"`
		Guidance  string `json:"guidance"`
	}) (store.Value, error) {
		return db.RecordProblemHint(in.ProblemID, in.Level, in.Guidance)
	}, false)
	register(server, "review_problem_draft", "Review the latest learner draft without revealing the answer.", func(in struct {
		ProblemID string `json:"problem_id"`
		Verdict   string `json:"verdict"`
		Feedback  string `json:"feedback"`
	}) (store.Value, error) {
		return db.ReviewProblemDraft(in.ProblemID, in.Verdict, in.Feedback)
	}, false)
	register(server, "finish_problem", "Conclude an attempt with a provisional assessment and justified concept credits.", func(in struct {
		ProblemID          string   `json:"problem_id"`
		Outcome            string   `json:"outcome"`
		Evaluation         string   `json:"evaluation"`
		Confidence         float64  `json:"confidence"`
		CreditedConceptIDs []string `json:"credited_concept_ids,omitempty"`
	}) (store.Value, error) {
		return db.FinishProblem(in.ProblemID, in.Outcome, in.Evaluation, in.Confidence, in.CreditedConceptIDs)
	}, false)
	register(server, "get_problem_solution", "Reveal a problem's answer and rubric only after closure.", func(in struct {
		ProblemID string `json:"problem_id"`
	}) (store.Value, error) {
		return db.GetProblemSolution(in.ProblemID)
	}, false)

	register(server, "show_reasoning_graph", "Inspect source-grounded propositions and multi-premise reasoning chains.", func(in struct {
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 150
		}
		data, err := db.ReasoningGraph(limit)
		return withView("reasoning", data), err
	}, true)
	register(server, "show_practice_session", "Open practice progression and interleaved exercise history.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		data, err := db.GetPracticeSession(in.SessionID)
		return withView("practice", data), err
	}, true)
	register(server, "show_learning_sources", "Open the source library and extraction coverage.", func(in struct {
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 30
		}
		data, err := db.ListSources(limit)
		return withView("sources", data), err
	}, true)
	register(server, "ingest_source_text", "Store exact source text as auditable spans without awarding mastery. For PDFs or pictures, send faithfully extracted text with page or diagram provenance.", func(in struct {
		Label    string `json:"label"`
		Kind     string `json:"kind"`
		MimeType string `json:"mime_type"`
		Text     string `json:"text"`
	}) (store.Value, error) {
		return db.IngestText(in.Label, in.Kind, in.MimeType, in.Text)
	}, false)
	register(server, "list_learning_sources", "List source documents and extraction coverage.", func(in struct {
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 30
		}
		return db.ListSources(limit)
	}, false)
	register(server, "preview_source_curriculum", "Preview a prerequisite-ordered curriculum from fully audited and reviewed source claims without awarding mastery.", func(in struct {
		SourceID string `json:"source_id"`
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		if in.Limit == 0 { in.Limit = 80 }
		return db.PreviewSourceCurriculum(in.SourceID, in.Limit)
	}, false)
	register(server, "import_source_curriculum", "Import a reviewed source curriculum as versioned concepts and prerequisites only after confirming its exact digest. No mastery is awarded.", func(in struct {
		SourceID string `json:"source_id"`
		Limit int `json:"limit,omitempty"`
		ExpectedSHA256 string `json:"expected_sha256"`
		Confirm bool `json:"confirm"`
	}) (store.Value, error) {
		if in.Limit == 0 { in.Limit = 80 }
		return db.ImportSourceCurriculum(in.SourceID, in.Limit, in.ExpectedSHA256, in.Confirm)
	}, false)
	register(server, "next_extraction", "Retrieve the next uncovered source span or a pending identity audit.", func(in struct {
		SourceID string `json:"source_id"`
	}) (store.Value, error) {
		return db.NextExtraction(in.SourceID)
	}, false)
	register(server, "propose_extraction", "Propose exact-quote-grounded propositions and joint-premise inference chains. This is not approval or learner mastery.", func(in struct {
		SpanID string                      `json:"span_id"`
		Claims []store.ExtractionCandidate `json:"claims"`
		Chains []store.ExtractionChain     `json:"chains,omitempty"`
	}) (store.Value, error) {
		return db.ProposeExtraction(in.SpanID, store.ExtractionProposal{Claims: in.Claims, Chains: in.Chains})
	}, false)
	register(server, "audit_extraction", "Separately audit source-faithfulness and proposition identity, then merge deterministically; never award learner mastery.", func(in struct {
		BatchID   string                     `json:"batch_id"`
		Decisions []store.ExtractionDecision `json:"decisions"`
		Chains    []store.ChainDecision      `json:"chains"`
	}) (store.Value, error) {
		return db.AuditExtraction(in.BatchID, in.Decisions, in.Chains)
	}, false)
	register(server, "reject_extraction", "Reject an unfaithful extraction batch so it cannot enter the graph.", func(in struct {
		BatchID string `json:"batch_id"`
		Reason  string `json:"reason"`
	}) (store.Value, error) {
		return db.RejectExtraction(in.BatchID, in.Reason)
	}, false)
	register(server, "get_reasoning_graph", "Read provisional propositions, source-grounded multi-premise chains, and review state; separate from mastery evidence.", func(in struct {
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 150
		}
		return db.ReasoningGraph(limit)
	}, false)
	register(server, "get_reasoning_chain", "Inspect all jointly required premises, provenance, review history and current review blockers. Review does not certify proof soundness.", func(in struct {
		ChainID string `json:"chain_id"`
	}) (store.Value, error) {
		return db.GetReasoningChain(in.ChainID)
	}, false)
	register(server, "check_reasoning_entailment", "Exhaustively check a bound propositional formalization of every premise and conclusion. Return counterexamples and immutable results; never certify the translation or learner mastery.", func(in struct {
		ChainID       string                       `json:"chain_id"`
		Formalization store.ReasoningFormalization `json:"formalization"`
	}) (store.Value, error) {
		return db.CheckReasoningEntailment(in.ChainID, in.Formalization)
	}, false)
	register(server, "get_reasoning_formal_checks", "Read retained propositional check results for a chain, including stale-claim flags. These do not certify natural-language entailment.", func(in struct {
		ChainID string `json:"chain_id"`
		Limit   int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 10
		}
		return db.GetReasoningFormalChecks(in.ChainID, limit)
	}, false)
	register(server, "record_translation_review", "Record a source-anchored translation judgment for every premise and conclusion of an exact propositional check. Reviewer identity and semantic correctness are not independently authenticated.", func(in struct {
		CheckID string                  `json:"check_id"`
		Review  store.TranslationReview `json:"review"`
	}) (store.Value, error) {
		return db.RecordTranslationReview(in.CheckID, in.Review)
	}, false)
	register(server, "get_translation_reviews", "Read append-only source-anchored review judgments and current source/claim integrity; these are not independent truth certificates.", func(in struct {
		CheckID string `json:"check_id"`
		Limit   int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 10
		}
		return db.GetTranslationReviews(in.CheckID, limit)
	}, false)
	register(server, "record_proposition_dispute", "Record a source or model disagreement without presuming either proposition is false.", func(in struct {
		LeftID  string `json:"left_id"`
		RightID string `json:"right_id"`
		Reason  string `json:"reason"`
	}) (store.Value, error) {
		return db.RecordPropositionDispute(in.LeftID, in.RightID, in.Reason)
	}, false)
	register(server, "resolve_proposition_dispute", "Document whether apparently conflicting propositions apply to different contexts or require further evidence; never award mastery.", func(in struct {
		DisputeID   string `json:"dispute_id"`
		Status      string `json:"status"`
		Explanation string `json:"explanation"`
	}) (store.Value, error) {
		return db.ResolvePropositionDispute(in.DisputeID, in.Status, in.Explanation)
	}, false)
	register(server, "get_proposition", "Read a proposition, original quotations and source references, without inferring learner mastery.", func(in struct {
		PropositionID string `json:"proposition_id"`
	}) (store.Value, error) {
		return db.GetProposition(in.PropositionID)
	}, false)
	register(server, "review_proposition", "Record an explicit source-verification judgment, not a learner assessment.", func(in struct {
		PropositionID string `json:"proposition_id"`
		Status        string `json:"status"`
		Explanation   string `json:"explanation"`
	}) (store.Value, error) {
		return db.ReviewProposition(in.PropositionID, in.Status, in.Explanation)
	}, false)
	register(server, "review_reasoning_chain", "Record whether a documented multi-premise connection follows, is merely supportive, or is disputed.", func(in struct {
		ChainID     string `json:"chain_id"`
		Status      string `json:"status"`
		Explanation string `json:"explanation"`
	}) (store.Value, error) {
		return db.ReviewReasoningChain(in.ChainID, in.Status, in.Explanation)
	}, false)
	register(server, "search_learning_sources", "Search private text spans by literal phrase for retrieval-augmented teaching.", func(in struct {
		Term  string `json:"term"`
		Limit int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 12
		}
		return db.SearchSources(in.Term, limit)
	}, false)
	register(server, "search_knowledge", "Retrieve relevant source passages with indexed lexical ranking; quote and cite the returned provenance.", func(in struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 12
		}
		return db.SearchKnowledge(in.Query, limit)
	}, false)
	register(server, "append_learning_event", "Persist an actual source/learner/tutor turn for incremental, source-audited graph extraction.", func(in struct {
		SessionID string `json:"session_id"`
		OriginID  string `json:"origin_id"`
		Role      string `json:"role"`
		Content   string `json:"content"`
	}) (store.Value, error) {
		return db.AppendLearningEvent(in.SessionID, in.OriginID, in.Role, in.Content)
	}, false)
	register(server, "get_learning_events", "Read a bounded transcript from a durable learning session.", func(in struct {
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 25
		}
		return db.LearningEvents(in.SessionID, limit)
	}, false)
	register(server, "upload_attachment", "Preserve the original user-supplied photo, diagram, text or PDF bytes privately for an existing problem/session/source. Base64 input is required.", func(in struct {
		OwnerKind string `json:"owner_kind"`
		OwnerID   string `json:"owner_id"`
		Filename  string `json:"filename"`
		MimeType  string `json:"mime_type"`
		Base64    string `json:"base64"`
		Note      string `json:"note,omitempty"`
	}) (store.Value, error) {
		return db.UploadAttachment(in.OwnerKind, in.OwnerID, in.Filename, in.MimeType, in.Base64, in.Note)
	}, false)
	register(server, "list_attachments", "List stored original-file metadata and tentative interpretations for a learning record.", func(in struct {
		OwnerKind string `json:"owner_kind"`
		OwnerID   string `json:"owner_id"`
	}) (store.Value, error) {
		return db.ListAttachments(in.OwnerKind, in.OwnerID)
	}, false)
	register(server, "get_attachment", "Retrieve attachment metadata and optionally original bytes; use only when the learner authorizes inspection.", func(in struct {
		AttachmentID    string `json:"attachment_id"`
		IncludeOriginal bool   `json:"include_original,omitempty"`
	}) (store.Value, error) {
		return db.GetAttachment(in.AttachmentID, in.IncludeOriginal)
	}, false)
	register(server, "annotate_attachment", "Record a tentative model observation, learner transcription or verified transcription. This never awards mastery.", func(in struct {
		AttachmentID string `json:"attachment_id"`
		Text         string `json:"text"`
		Method       string `json:"method"`
		Uncertainty  string `json:"uncertainty,omitempty"`
	}) (store.Value, error) {
		return db.AnnotateAttachment(in.AttachmentID, in.Text, in.Method, in.Uncertainty)
	}, false)
	register(server, "start_practice_session", "Create a durable interleaved practice session for selected known concepts.", func(in struct {
		Goal       string   `json:"goal"`
		ConceptIDs []string `json:"concept_ids"`
	}) (store.Value, error) {
		return db.StartPracticeSession(in.Goal, in.ConceptIDs)
	}, false)
	register(server, "get_practice_session", "Read practice targets, completed demands, and remaining active work.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		return db.GetPracticeSession(in.SessionID)
	}, false)
	register(server, "list_practice_sessions", "List durable practice sessions.", func(in struct {
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 25
		}
		return db.ListPracticeSessions(limit)
	}, false)
	register(server, "recommend_practice", "Select a retrieval/discrimination/composition/solve target and difficulty without exposing the future problem's answer.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		return db.RecommendPractice(in.SessionID)
	}, false)
	register(server, "create_practice_problem", "Pose one fresh practice situation with a sealed reference solution and rubric. No second active problem is allowed.", func(in struct {
		SessionID         string `json:"session_id"`
		TargetConceptID   string `json:"target_concept_id"`
		Method            string `json:"method"`
		CueClass          string `json:"cue_class"`
		Difficulty        int    `json:"difficulty"`
		Title             string `json:"title"`
		Statement         string `json:"statement"`
		Kind              string `json:"kind"`
		Discipline        string `json:"discipline,omitempty"`
		ReferenceSolution string `json:"reference_solution"`
		GradingRubric     string `json:"grading_rubric"`
	}) (store.Value, error) {
		return db.CreatePracticeProblem(in.SessionID, in.TargetConceptID, in.Method, in.CueClass, in.Difficulty, in.Title, in.Statement, in.Kind, in.Discipline, in.ReferenceSolution, in.GradingRubric)
	}, false)
	register(server, "record_practice_outcome", "After closing an actual problem, classify the failure and schedule fresh-surface retrieval.", func(in struct {
		PracticeItemID string `json:"practice_item_id"`
		Correct        bool   `json:"correct"`
		MissClass      string `json:"miss_class"`
		Remarks        string `json:"remarks"`
	}) (store.Value, error) {
		return db.RecordPracticeOutcome(in.PracticeItemID, in.Correct, in.MissClass, in.Remarks)
	}, false)
	register(server, "get_review_queue", "Read delayed retrieval obligations; does not create future tasks automatically.", func(in struct {
		Limit           int  `json:"limit,omitempty"`
		IncludeUpcoming bool `json:"include_upcoming,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 25
		}
		return db.GetReviewQueue(limit, in.IncludeUpcoming)
	}, false)
	register(server, "close_practice_session", "Close a practice session without discarding historical attempts.", func(in struct {
		SessionID string `json:"session_id"`
	}) (store.Value, error) {
		return db.ClosePracticeSession(in.SessionID)
	}, false)
	mcp.AddTool(server, &mcp.Tool{Name: "inspect_attachment", Description: "Display an authorized original photograph or diagram as an image to the reasoning model; image interpretation is not learner mastery."},
		func(_ context.Context, _ *mcp.CallToolRequest, in struct {
			AttachmentID string `json:"attachment_id"`
		}) (*mcp.CallToolResult, any, error) {
			record, err := db.GetAttachment(in.AttachmentID, true)
			if err != nil {
				return nil, nil, err
			}
			typ := record["mime_type"].(string)
			if !strings.HasPrefix(typ, "image/") {
				return nil, nil, fmt.Errorf("attachment is not an inspectable image: %s", typ)
			}
			original, err := base64.StdEncoding.Strict().DecodeString(record["base64"].(string))
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: original, MIMEType: typ}}}, nil, nil
		})
	register(server, "record_problem_check", "Record a provisional checker claim about the latest learner draft. This is not authenticated verification or mastery evidence.", func(in struct {
		ProblemID         string `json:"problem_id"`
		Method            string `json:"method"`
		Outcome           string `json:"outcome"`
		EvidenceReference string `json:"evidence_reference"`
		Notes             string `json:"notes"`
	}) (store.Value, error) {
		return db.RecordProblemCheck(in.ProblemID, in.Method, in.Outcome, in.EvidenceReference, in.Notes)
	}, false)
	register(server, "record_verified_problem_check", "Accept an authenticated checker receipt for the exact current learner draft. A signature verifies provenance, not proof soundness.", func(in struct {
		Receipt   store.VerificationReceipt `json:"receipt"`
		Signature string                    `json:"signature"`
	}) (store.Value, error) {
		return db.RecordVerifiedProblemCheck(in.Receipt, in.Signature)
	}, false)
	register(server, "get_confidence_calibration", "Compare learner-reported certainty with objectively keyed choices and authenticated verifier outcomes. Reports are descriptive and do not award mastery.", func(in struct {
		ConceptID string `json:"concept_id,omitempty"`
		Limit     int    `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 250
		}
		return db.ConfidenceCalibration(in.ConceptID, limit)
	}, false)
	register(server, "get_mastery_portfolio", "Inspect independent difficulty, novelty, variety, delayed retention and authenticated corroboration before making a mastery claim.", func(in struct {
		ConceptID string `json:"concept_id"`
	}) (store.Value, error) {
		return db.MasteryPortfolio(in.ConceptID)
	}, false)
	register(server, "get_reassessment_queue", "Find foundational reconstructions due for a delayed independent reassessment.", func(in struct {
		Limit int `json:"limit,omitempty"`
	}) (store.Value, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 25
		}
		return db.DueReassessments(limit)
	}, false)
    registerModelTools(svc)
    return svc
}

// Handler serves the local MCP transport. Remote listeners must use
// HandlerWithBearer to authenticate every MCP request.
func (svc *Service) Handler(allowedHosts, allowedOrigins string) http.Handler {
	return svc.HandlerWithBearer(allowedHosts, allowedOrigins, "")
}

// HandlerWithBearer checks Host, Origin, and the configured bearer credential.
func (svc *Service) HandlerWithBearer(allowedHosts, allowedOrigins, bearerToken string) http.Handler {
	return svc.HandlerWithAccess(allowedHosts, allowedOrigins, bearerToken, nil)
}

// HandlerWithAccess accepts independently verified Cloudflare Access assertions
// while retaining the owner-only static credential for direct local clients.
func (svc *Service) HandlerWithAccess(allowedHosts, allowedOrigins, bearerToken string, access *AccessVerifier) http.Handler {
	hosts := strings.Split(allowedHosts, ",")
	origins := map[string]bool{}
	for _, s := range strings.Split(allowedOrigins, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			origins[s] = true
		}
	}
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return svc.Server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdk)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path,"/mcp-assets/") {
            if svc.bundle==nil {http.NotFound(w,r);return}
            svc.bundle.ServeStatic(w,r)
            return
        }
		ok := false
		for _, entry := range hosts {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if entry == r.Host || (strings.HasSuffix(entry, ":*") && strings.HasPrefix(r.Host, strings.TrimSuffix(entry, "*"))) {
				ok = true
				break
			}
		}
		if !ok {
			http.Error(w, "unapproved Host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !origins[origin] {
			http.Error(w, "unapproved Origin", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/mcp" && r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/mcp" {
			provided := []byte(r.Header.Get("Authorization"))
			expected := []byte("Bearer " + bearerToken)
			ok := bearerToken != "" && subtle.ConstantTimeCompare(provided, expected) == 1
			if !ok && access != nil {
				ok = access.Verify(r.Context(), r.Header.Get("Cf-Access-Jwt-Assertion")) == nil
			}
			if !ok && (bearerToken != "" || access != nil) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="adaptive-teaching"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		if r.URL.Path == "/mcp" && r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
		}
		mux.ServeHTTP(w, r)
	})
}
