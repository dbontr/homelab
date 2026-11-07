package store

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newExtendedStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "learning.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func fail(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected rejection")
	}
}
func TestSourceExtractionAndEvidenceGraph(t *testing.T) {
	mustResult := func(value Value, err error) Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	s := newExtendedStore(t)
	text := "A derivative is defined by a limit of difference quotients. A limit requires controlling the error through an epsilon-delta statement. An algebraic difference quotient alone is not a derivative."
	source := mustResult(s.IngestText("Lecture notes", "notes", "text/markdown", text))
	if source["duplicate"].(bool) {
		t.Fatal("first source duplicate")
	}
	duplicate := mustResult(s.IngestText("Copy", "notes", "text/markdown", text))
	if duplicate["id"] != source["id"] || !duplicate["duplicate"].(bool) {
		t.Fatal("idempotency failed")
	}
	work := mustResult(s.NextExtraction(source["id"].(string)))
	if work["action"] != "extract" {
		t.Fatal(work)
	}
	span := work["span_id"].(string)
	fail(t, func() error {
		_, e := s.ProposeExtraction(span, ExtractionProposal{Claims: []ExtractionCandidate{{Statement: "Unsupported claim", Quote: "Not in source"}}})
		return e
	}())
	proposal := ExtractionProposal{Claims: []ExtractionCandidate{
		{Statement: "The derivative is defined by a limit of difference quotients", Domain: "analysis", Context: "elementary real analysis", Quote: "A derivative is defined by a limit of difference quotients"},
		{Statement: "A limit requires controlling the error through an epsilon-delta statement", Domain: "analysis", Context: "elementary real analysis", Quote: "A limit requires controlling the error through an epsilon-delta statement"},
	}, Chains: []ExtractionChain{{Conclusion: 0, Premises: []int{1}, Relation: "supports", Explanation: "Limits ground derivative definitions"}}}
	staged := mustResult(s.ProposeExtraction(span, proposal))
	if staged["status"] != "pending_audit" {
		t.Fatal(staged)
	}
	queued := mustResult(s.NextExtraction(source["id"].(string)))
	if queued["action"] != "audit" {
		t.Fatal(queued)
	}
	fail(t, func() error {
		_, e := s.AuditExtraction(staged["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, nil)
		return e
	}())
	merged := mustResult(s.AuditExtraction(staged["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}, {Index: 1, Action: "accept"}}, []ChainDecision{{Index: 0, Keep: true}}))
	if merged["accepted_claims"] != 2 || merged["accepted_chains"] != 1 {
		t.Fatal(merged)
	}
	again := mustResult(s.AuditExtraction(staged["batch_id"].(string), nil, nil))
	if again["duplicate"] != true {
		t.Fatal("audit replay changed results")
	}
	done := mustResult(s.NextExtraction(source["id"].(string)))
	if done["action"] != "complete" {
		t.Fatal(done)
	}
	graph := mustResult(s.ReasoningGraph(50))
	nodes := graph["propositions"].([]Value)
	if len(nodes) != 2 || len(graph["chains"].([]Value)) != 1 {
		t.Fatal(graph)
	}
	record := mustResult(s.GetProposition(nodes[0]["id"].(string)))
	if len(record["provenance"].([]Value)) != 1 {
		t.Fatal(record)
	}
	if record["learner_mastery"] != nil {
		t.Fatal("source wrongly certified learner")
	}
	review := mustResult(s.ReviewProposition(nodes[0]["id"].(string), "reviewed", "Evidence matches the source"))
	if review["mastery_unchanged"] != true {
		t.Fatal(review)
	}
}
func TestPracticeProgressionAndRetests(t *testing.T) {
	mustResult := func(value Value, err error) Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	s := newExtendedStore(t)
	mustResult(s.AddConcept("limits", "Limits", "Understanding limits", "analysis"))
	mustResult(s.AddConcept("sequences", "Sequences", "Sequential reasoning", "analysis"))
	run := mustResult(s.StartPracticeSession("Analysis", []string{"limits", "sequences"}))
	id := run["id"].(string)
	plan := mustResult(s.RecommendPractice(id))
	if plan["action"] != "pose" || plan["method"] != "recall" {
		t.Fatal(plan)
	}
	cid := plan["target_concept_id"].(string)
	item := mustResult(s.CreatePracticeProblem(id, cid, "recall", "first-surface", 2, "First", "Show an unfamiliar example", "proof", "analysis", "Use definitions", "Check each argument"))
	pid := item["exercise"].(Value)["id"].(string)
	blocked := mustResult(s.RecommendPractice(id))
	if blocked["action"] != "finish_active_problem" {
		t.Fatal(blocked)
	}
	fail(t, func() error {
		_, e := s.CreatePracticeProblem(id, cid, "recall", "other-cue", 2, "Second", "Different question", "proof", "analysis", "Solution", "Rubric")
		return e
	}())
	mustResult(s.SaveProblemDraft(pid, "Incomplete, I cannot prove this yet."))
	mustResult(s.FinishProblem(pid, "incomplete", "Not sufficient", .8, nil))
	outcome := mustResult(s.RecordPracticeOutcome(item["practice_item_id"].(string), false, "execution", "Incorrect quantifier order"))
	if outcome["miss_class"] != "execution" {
		t.Fatal(outcome)
	}
	fail(t, func() error {
		_, e := s.RecordPracticeOutcome(item["practice_item_id"].(string), false, "cue", "Duplicate")
		return e
	}())
	fail(t, func() error {
		_, e := s.CreatePracticeProblem(id, cid, "recall", "first-surface", 1, "Duplicate", "Different question", "proof", "analysis", "Solution", "Rubric")
		return e
	}())
	_, err := s.GetReviewQueue(15, true)
	if err != nil {
		t.Fatal(err)
	}
	closed := mustResult(s.ClosePracticeSession(id))
	if closed["state"] != "closed" {
		t.Fatal(closed)
	}
}
func TestAttachmentOriginalAndTranscriptionSeparate(t *testing.T) {
	mustResult := func(value Value, err error) Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	s := newExtendedStore(t)
	mustResult(s.AddConcept("diagram", "Diagrams", "Circuit diagrams", "engineering"))
	session := mustResult(s.StartLearningSession("Read this circuit", "diagram", "learn", nil))
	image := append([]byte("\x89PNG\r\n\x1a\n"), []byte("image pixels and header")...)
	encoded := base64.StdEncoding.EncodeToString(image)
	saved := mustResult(s.UploadAttachment("session", session["id"].(string), "schematic.png", "image/png", encoded, "Breadboard image"))
	id := saved["id"].(string)
	if saved["size_bytes"] != int64(len(image)) {
		t.Fatal(saved)
	}
	reject, err := s.UploadAttachment("session", session["id"].(string), "x.pdf", "application/pdf", encoded, "")
	_ = reject
	fail(t, err)
	duplicate := mustResult(s.UploadAttachment("session", session["id"].(string), "schematic.png", "image/png", encoded, ""))
	if duplicate["duplicate"] != true {
		t.Fatal(duplicate)
	}
	annotated := mustResult(s.AnnotateAttachment(id, "The left resistor might be 10 ohms", "model_interpretation", "Color bands hard to read"))
	if annotated["mastery_unchanged"] != true {
		t.Fatal(annotated)
	}
	retrieved := mustResult(s.GetAttachment(id, true))
	if retrieved["base64"] != encoded || len(retrieved["annotations"].([]Value)) != 1 {
		t.Fatal(retrieved)
	}
	listed := mustResult(s.ListAttachments("session", session["id"].(string)))
	if len(listed["attachments"].([]Value)) != 1 {
		t.Fatal(listed)
	}
}
func TestSessionEventExtractionIdempotent(t *testing.T) {
	mustResult := func(value Value, err error) Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	s := newExtendedStore(t)
	mustResult(s.AddConcept("gravity", "Gravity", "A physical phenomenon", "physics"))
	session := mustResult(s.StartLearningSession("Understand gravity", "gravity", "learn", nil))
	sid := session["id"].(string)
	first := mustResult(s.AppendLearningEvent(sid, "turn-1", "learner", "I think the force decreases with distance."))
	second := mustResult(s.AppendLearningEvent(sid, "turn-1", "learner", "I think the force decreases with distance."))
	if first["event_id"] != second["event_id"] || second["duplicate"] != true {
		t.Fatal(first, second)
	}
	fail(t, func() error { _, e := s.AppendLearningEvent(sid, "turn-1", "tutor", "Different content"); return e }())
	ev := mustResult(s.LearningEvents(sid, 10))
	if len(ev["events"].([]Value)) != 1 {
		t.Fatal(ev)
	}
	work := mustResult(s.NextExtraction(first["source_id"].(string)))
	if work["action"] != "extract" || !strings.Contains(work["content"].(string), "force decreases") {
		t.Fatal(work)
	}
	matched := mustResult(s.SearchSources("force decreases", 10))
	if len(matched["matches"].([]Value)) != 1 {
		t.Fatal(matched)
	}
}

func TestFullMasteryRequiresVariedDifficultWorkAndChecks(t *testing.T) {
	s, signer := trustedTestStore(t)
	mustResult := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	mustResult(s.AddConcept("derivative", "Derivative", "Derivative construction", "analysis"))
	for _, d := range []string{"recognition", "application", "justification", "transfer", "independent_reconstruction"} {
		mustResult(s.RecordEvidence("derivative", d, "Independent exercise", "Reasoned argument", "Correct", true, .98, false))
	}
	_, err := s.with(func(db *database) Value {
		exec(db, "UPDATE evidence SET recorded_at=? WHERE concept_id='derivative' AND dimension='independent_reconstruction'", time.Now().UTC().Add(-30*time.Hour).Format("2006-01-02T15:04:05.000000+00:00"))
		return Value{}
	})
	if err != nil {
		t.Fatal(err)
	}
	mustResult(s.RecordEvidence("derivative", "retention", "Delayed reconstruction", "Independent argument", "Correct", true, .98, false))
	firstProblem := ""
	secondProblem := ""
	for i := 0; i < 3; i++ {
		kind := "proof"
		if i == 2 {
			kind = "derivation"
		}
		p := mustResult(s.CreateProblem(fmt.Sprintf("Novel challenge %d", i), fmt.Sprintf("Derive result from unique data %d without a known template", i), kind, "independent", "challenging", []string{"derivative"}, "analysis", "generated", "Reference reasoning", "Check all steps", ""))
		id := p["id"].(string)
		mustResult(s.SaveProblemDraft(id, "Independent solution with all steps explained"))
		if i == 0 {
			firstProblem = id
			mustResult(s.RecordProblemCheck(id, "symbolic", "pass", "CAS run evidence-123", "Reported symbolic check"))
		}
		if i == 1 {
			secondProblem = id
		}
		mustResult(s.FinishProblem(id, "solved", "Valid independent solution", .95, []string{"derivative"}))
		observed := mustResult(s.Concept("derivative"))["mastery"].(Value)
		if i < 2 && observed["meets_target"].(bool) {
			t.Fatalf("premature mastery after %d problems: %#v", i+1, observed)
		}
	}
	unverified := mustResult(s.Concept("derivative"))["mastery"].(Value)
	if unverified["meets_target"] != false {
		t.Fatal("an unsigned external claim cannot establish full mastery", unverified)
	}
	receipt, signature := signLatestDraft(t, s, signer, firstProblem, "symbolic", "pass")
	mustResult(s.RecordVerifiedProblemCheck(receipt, signature))
	m := mustResult(s.Concept("derivative"))["mastery"].(Value)
	if m["level"] != 5 || m["meets_target"] != true {
		t.Fatal(m)
	}
	report := mustResult(s.MasteryPortfolio("derivative"))
	if report["distinct_difficult_solutions"] != 3 || report["full_mastery_evidence_recorded"] != true {
		t.Fatal(report)
	}
	otherPass, otherSignature := signLatestDraft(t, s, signer, secondProblem, "symbolic", "pass")
	mustResult(s.RecordVerifiedProblemCheck(otherPass, otherSignature))
	otherFailure, failureSignature := signLatestDraft(t, s, signer, secondProblem, "symbolic", "fail")
	mustResult(s.RecordVerifiedProblemCheck(otherFailure, failureSignature))
	contradicted := mustResult(s.Concept("derivative"))["mastery"].(Value)
	if contradicted["meets_target"] != false {
		t.Fatal("a contradictory authenticated result must prevent full mastery", contradicted)
	}
	report = mustResult(s.MasteryPortfolio("derivative"))
	if report["conflicting_checker_reports"] != 1 || report["full_mastery_evidence_recorded"] != false {
		t.Fatal("conflicting checks were not included in the mastery gate", report)
	}
}
func TestSearchRetrievalAndRetriedExtraction(t *testing.T) {
	s := newExtendedStore(t)
	mustResult := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	source := mustResult(s.IngestText("Griffiths notes", "document", "text/plain", "Electrostatic potential is a scalar field. The gradient produces electric field with a negative sign."))
	id := source["id"].(string)
	matches := mustResult(s.SearchKnowledge("electrostatic scalar", 10))["matches"].([]Value)
	if len(matches) != 1 {
		t.Fatal("FTS index absent or no ranked results", matches)
	}
	harmless := mustResult(s.SearchKnowledge(`field" OR DROP TABLE`, 10))["matches"].([]Value)
	_ = harmless
	work := mustResult(s.NextExtraction(id))
	spanID := work["span_id"].(string)
	candidate := ExtractionCandidate{Statement: "Potential is a scalar field", Context: "electrostatics", Domain: "physics", Quote: "Electrostatic potential is a scalar field"}
	batch := mustResult(s.ProposeExtraction(spanID, ExtractionProposal{Claims: []ExtractionCandidate{candidate}}))
	mustResult(s.RejectExtraction(batch["batch_id"].(string), "Insufficient precision in the claim"))
	candidate.Statement = "Electrostatic potential is a scalar field"
	retry := mustResult(s.ProposeExtraction(spanID, ExtractionProposal{Claims: []ExtractionCandidate{candidate}}))
	if retry["status"] != "pending_audit" {
		t.Fatal("cannot retry rejected extraction", retry)
	}
	mustResult(s.AuditExtraction(retry["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, []ChainDecision{}))
	if mustResult(s.NextExtraction(id))["action"] != "complete" {
		t.Fatal("span not covered")
	}
}

func TestAttachmentOriginalIntegrity(t *testing.T) {
	s := newExtendedStore(t)
	if _, err := s.AddConcept("originals", "Originals", "Preserved learner work", "practical"); err != nil {
		t.Fatal(err)
	}
	session, err := s.StartLearningSession("Preserve original material", "originals", "learn", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF\n")
	saved, err := s.UploadAttachment("session", session["id"].(string), "notebook.pdf", "application/pdf", base64.StdEncoding.EncodeToString(body), "")
	if err != nil {
		t.Fatal(err)
	}
	id := saved["id"].(string)
	original, err := s.GetAttachment(id, true)
	if err != nil || original["base64"] != base64.StdEncoding.EncodeToString(body) || original["integrity_verified"] != true {
		t.Fatalf("valid original was not verified: record=%v err=%v", original, err)
	}

	change := func(query string, value any) {
		t.Helper()
		if _, err := s.with(func(db *database) Value {
			exec(db, query, value, id)
			return Value{}
		}); err != nil {
			t.Fatal(err)
		}
	}
	expectRejected := func(part string) {
		t.Helper()
		if _, err := s.GetAttachment(id, true); err == nil || !strings.Contains(err.Error(), part) {
			t.Fatalf("corrupted original must be rejected with %q: %v", part, err)
		}
	}

	corrupted := append([]byte(nil), body...)
	corrupted[len(corrupted)-2] ^= 1
	change("UPDATE attachments SET data=? WHERE id=?", corrupted)
	expectRejected("checksum mismatch")

	metadata, err := s.GetAttachment(id, false)
	if err != nil || metadata["integrity_verified"] != nil || metadata["base64"] != nil {
		t.Fatalf("metadata cannot attest to original integrity: record=%v err=%v", metadata, err)
	}

	change("UPDATE attachments SET data=? WHERE id=?", body)
	change("UPDATE attachments SET size_bytes=? WHERE id=?", len(body)-1)
	expectRejected("size mismatch")

	change("UPDATE attachments SET size_bytes=? WHERE id=?", len(body))
	change("UPDATE attachments SET sha256=? WHERE id=?", strings.Repeat("0", 64))
	expectRejected("checksum mismatch")

	change("UPDATE attachments SET sha256=? WHERE id=?", saved["sha256"].(string))
	restored, err := s.GetAttachment(id, true)
	if err != nil || restored["integrity_verified"] != true {
		t.Fatalf("restored original failed integrity verification: record=%v err=%v", restored, err)
	}
}
