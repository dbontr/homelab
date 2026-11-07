package store

import (
	"strings"
	"testing"
)

func reviewedChainFixture(t *testing.T) (*Store, map[string]string, string) {
	t.Helper()
	s := newExtendedStore(t)
	sourceText := "For a resistor, voltage equals current multiplied by resistance. A complete loop has zero net voltage change. These two facts imply that the source balances the resistor's drop. In a separate experiment, a changing magnetic field produced an induced voltage."
	source, err := s.IngestText("Circuit observations", "notes", "text/plain", sourceText)
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.NextExtraction(source["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	claims := []ExtractionCandidate{
		{Statement: "Ohm's law", Context: "resistive circuits", Domain: "circuits", Quote: "For a resistor, voltage equals current multiplied by resistance."},
		{Statement: "Kirchhoff's voltage law", Context: "closed loops", Domain: "circuits", Quote: "A complete loop has zero net voltage change."},
		{Statement: "Source and resistive drop balance", Context: "resistive loops", Domain: "circuits", Quote: "These two facts imply that the source balances the resistor's drop."},
		{Statement: "Induced electromotive force", Context: "time-varying fields", Domain: "circuits", Quote: "In a separate experiment, a changing magnetic field produced an induced voltage."},
	}
	batch, err := s.ProposeExtraction(work["span_id"].(string), ExtractionProposal{
		Claims: claims,
		Chains: []ExtractionChain{{Conclusion: 2, Premises: []int{0, 1}, Relation: "supports", Explanation: "Both a resistor law and a loop balance are required."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions := make([]ExtractionDecision, len(claims))
	for i := range decisions {
		decisions[i] = ExtractionDecision{Index: i, Action: "accept"}
	}
	if _, err := s.AuditExtraction(batch["batch_id"].(string), decisions, []ChainDecision{{Index: 0, Keep: true}}); err != nil {
		t.Fatal(err)
	}
	graph, err := s.ReasoningGraph(20)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]string{}
	for _, n := range graph["propositions"].([]Value) {
		nodes[n["statement"].(string)] = n["id"].(string)
	}
	chains := graph["chains"].([]Value)
	if len(chains) != 1 || len(chains[0]["premises"].([]string)) != 2 {
		t.Fatalf("source reasoning chain must keep both premises: %v", chains)
	}
	return s, nodes, chains[0]["id"].(string)
}

func chainReviewState(t *testing.T, s *Store, id string) Value {
	t.Helper()
	record, err := s.GetReasoningChain(id)
	if err != nil {
		t.Fatal(err)
	}
	return record["chain"].(Value)
}

func hasReviewBlocker(chain Value, prefix string) bool {
	for _, b := range chain["review_blockers"].([]string) {
		if strings.HasPrefix(b, prefix) {
			return true
		}
	}
	return false
}

func TestReasoningChainReviewRequiresJointPremises(t *testing.T) {
	s, nodes, id := reviewedChainFixture(t)
	chain := chainReviewState(t, s, id)
	if chain["review_eligible"] != false || chain["review_active"] != false {
		t.Fatal("extraction alone must not establish a reviewed inference", chain)
	}
	if _, err := s.ReviewReasoningChain(id, "reviewed", "Premises are sufficient"); err == nil {
		t.Fatal("unreviewed source claims must block chain review")
	}
	for _, statement := range []string{"Ohm's law", "Source and resistive drop balance"} {
		if _, err := s.ReviewProposition(nodes[statement], "reviewed", "Checked source scope"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReviewReasoningChain(id, "reviewed", "Still missing a premise"); err == nil {
		t.Fatal("review must require every premise, not merely one")
	}
	if _, err := s.ReviewProposition(nodes["Kirchhoff's voltage law"], "reviewed", "Checked loop assumption"); err != nil {
		t.Fatal(err)
	}
	review, err := s.ReviewReasoningChain(id, "reviewed", "Both laws are needed under the stated scope")
	if err != nil || review["review_active"] != true {
		t.Fatalf("complete review should be active: review=%v err=%v", review, err)
	}
	detail, err := s.GetReasoningChain(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail["premises"].([]string)) != 2 || len(detail["provenance"].([]Value)) != 1 {
		t.Fatal("review has lost joint premises or source evidence", detail)
	}
	history := detail["review_history"].([]Value)
	if len(history) != 1 || history[0]["explanation"] != "Both laws are needed under the stated scope" {
		t.Fatal("review rationale must be durable and failed attempts unrecorded", history)
	}
	graph, err := s.ReasoningGraph(20)
	if err != nil {
		t.Fatal(err)
	}
	if graph["chains"].([]Value)[0]["review_active"] != true || graph["mastery_separate"] != true {
		t.Fatal("graph must expose standing without awarding mastery", graph)
	}
}

func TestReasoningChainReviewReactsToNewDisputes(t *testing.T) {
	s, nodes, id := reviewedChainFixture(t)
	for _, statement := range []string{"Ohm's law", "Kirchhoff's voltage law", "Source and resistive drop balance"} {
		if _, err := s.ReviewProposition(nodes[statement], "reviewed", "Reviewed quoted context"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReviewReasoningChain(id, "reviewed", "Joint circuit argument"); err != nil {
		t.Fatal(err)
	}
	premise := nodes["Ohm's law"]
	other := nodes["Induced electromotive force"]
	dispute, err := s.RecordPropositionDispute(premise, other, "Different experimental settings require scope review")
	if err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != false || !hasReviewBlocker(chain, "dispute:") {
		t.Fatal("a new unresolved dispute must invalidate active review", chain)
	}
	disputeID := dispute["id"].(string)
	if _, err := s.ResolvePropositionDispute(disputeID, "different_contexts", "The sources describe different field conditions"); err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != true {
		t.Fatal("a resolved different-context dispute need not invalidate a scoped chain", chain)
	}
	unsupportedSide := "right_supported"
	supportedSide := "left_supported"
	if dispute["right_id"] == premise {
		unsupportedSide, supportedSide = "left_supported", "right_supported"
	}
	if _, err := s.ResolvePropositionDispute(disputeID, unsupportedSide, "The premise is not supported"); err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != false {
		t.Fatal("a negatively resolved dispute must block the affected premise", chain)
	}
	if _, err := s.ResolvePropositionDispute(disputeID, supportedSide, "The premise has stronger evidence"); err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != true {
		t.Fatal("support for the affected premise should restore eligibility", chain)
	}

	premiseDispute, err := s.RecordPropositionDispute(nodes["Ohm's law"], nodes["Kirchhoff's voltage law"], "These statements came from incompatible assumed domains")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolvePropositionDispute(premiseDispute["id"].(string), "different_contexts", "Distinct experimental contexts"); err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != false {
		t.Fatal("premises from incompatible contexts cannot jointly support one inference", chain)
	}
	if _, err := s.ReviewReasoningChain(id, "reviewed", "Ignore the domain mismatch"); err == nil {
		t.Fatal("a fresh reviewed status must reject incompatible-context premises")
	}
}

func TestReasoningChainSourceTamperingInvalidatesStanding(t *testing.T) {
	s, nodes, id := reviewedChainFixture(t)
	for _, statement := range []string{"Ohm's law", "Kirchhoff's voltage law", "Source and resistive drop balance"} {
		if _, err := s.ReviewProposition(nodes[statement], "reviewed", "Reviewed quoted context"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReviewReasoningChain(id, "reviewed", "Source-grounded inference"); err != nil {
		t.Fatal(err)
	}
	premise := nodes["Ohm's law"]
	var original string
	if _, err := s.with(func(db *database) Value {
		record := getRow(db, "SELECT quoted_text FROM proposition_provenance WHERE proposition_id=?", premise)
		original = strVal(record["quoted_text"])
		exec(db, "UPDATE proposition_provenance SET quoted_text=? WHERE proposition_id=?", "This quotation is not in the source", premise)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != false || !hasReviewBlocker(chain, "invalid_claim_provenance:") {
		t.Fatal("invalid source attribution must block review", chain)
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE proposition_provenance SET quoted_text=? WHERE proposition_id=?", original, premise)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != true {
		t.Fatal("restoring exact source evidence should restore standing", chain)
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE source_spans SET content=content || ? WHERE id=(SELECT source_span_id FROM chain_provenance WHERE chain_id=? LIMIT 1)", " altered", id)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if chain := chainReviewState(t, s, id); chain["review_active"] != false || !hasReviewBlocker(chain, "invalid_chain_provenance") {
		t.Fatal("mutating a source span without its fingerprint must block review", chain)
	}
}

func TestSourceIntegrityRecheckedBeforeExtractionAudit(t *testing.T) {
	s := newExtendedStore(t)
	const original = "An observation must remain attributable to its original source."
	source, err := s.IngestText("Observation", "observation", "text/plain", original)
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.NextExtraction(source["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := s.ProposeExtraction(work["span_id"].(string), ExtractionProposal{Claims: []ExtractionCandidate{
		{Statement: "Evidence requires provenance", Quote: original},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE knowledge_sources SET content=? WHERE id=?", original+" altered", source["id"])
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuditExtraction(batch["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, []ChainDecision{}); err == nil {
		t.Fatal("source mutation must block extraction audit")
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE knowledge_sources SET content=? WHERE id=?", original, source["id"])
		exec(db, "UPDATE source_spans SET content=content || ? WHERE id=?", " altered", work["span_id"])
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuditExtraction(batch["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, []ChainDecision{}); err == nil {
		t.Fatal("span mutation must block extraction audit")
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE source_spans SET content=? WHERE id=?", original, work["span_id"])
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuditExtraction(batch["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, []ChainDecision{}); err != nil {
		t.Fatalf("exactly restored source should pass audit: %v", err)
	}
}

func TestReasoningChainReviewHistorySurvivesReopen(t *testing.T) {
	s, nodes, id := reviewedChainFixture(t)
	for _, statement := range []string{"Ohm's law", "Kirchhoff's voltage law", "Source and resistive drop balance"} {
		if _, err := s.ReviewProposition(nodes[statement], "reviewed", "Checked source"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReviewReasoningChain(id, "reviewed", "Qualified source argument"); err != nil {
		t.Fatal(err)
	}
	path := s.path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	detail, err := reopened.GetReasoningChain(id)
	if err != nil || detail["chain"].(Value)["review_active"] != true {
		t.Fatalf("review standing must persist after database reopen: detail=%v err=%v", detail, err)
	}
	if _, err := reopened.ReviewReasoningChain(id, "disputed", "New counterexample requires review"); err != nil {
		t.Fatal(err)
	}
	detail, err = reopened.GetReasoningChain(id)
	if err != nil {
		t.Fatal(err)
	}
	history := detail["review_history"].([]Value)
	if len(history) != 2 || history[0]["status"] != "reviewed" || history[1]["status"] != "disputed" ||
		history[0]["explanation"] != "Qualified source argument" || detail["chain"].(Value)["review_active"] != false {
		t.Fatal("review changes must preserve historical rationale without retaining active status", detail)
	}
}

func TestTranscriptProvenanceUsesOriginalLearningEvents(t *testing.T) {
	s := newExtendedStore(t)
	if _, err := s.AddConcept("testimony", "Testimony", "Evaluating observations", "history"); err != nil {
		t.Fatal(err)
	}
	session, err := s.StartLearningSession("Examine a historical argument", "testimony", "learn", nil)
	if err != nil {
		t.Fatal(err)
	}
	sid := session["id"].(string)
	event, err := s.AppendLearningEvent(sid, "turn-1", "learner", "I think the letter establishes an earlier date.")
	if err != nil {
		t.Fatal(err)
	}
	sourceID := event["source_id"].(string)
	work, err := s.NextExtraction(sourceID)
	if err != nil || work["action"] != "extract" {
		t.Fatalf("a genuine transcript should be extractable: work=%v err=%v", work, err)
	}
	batch, err := s.ProposeExtraction(work["span_id"].(string), ExtractionProposal{
		Claims: []ExtractionCandidate{{Statement: "The letter is dated earlier", Context: "historical interpretation", Quote: "I think the letter establishes an earlier date."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE learning_events SET content=? WHERE session_id=? AND origin_id='turn-1'", "Altered testimony", sid)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuditExtraction(batch["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, []ChainDecision{}); err == nil {
		t.Fatal("an altered original event must invalidate extracted transcript evidence")
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE learning_events SET content=? WHERE session_id=? AND origin_id='turn-1'", "I think the letter establishes an earlier date.", sid)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuditExtraction(batch["batch_id"].(string), []ExtractionDecision{{Index: 0, Action: "accept"}}, []ChainDecision{}); err != nil {
		t.Fatalf("restored transcript source must pass audit: %v", err)
	}
	if _, err := s.AppendLearningEvent(sid, "turn-2", "tutor", "The date remains an open source-criticism question."); err != nil {
		t.Fatal(err)
	}
	next, err := s.NextExtraction(sourceID)
	if err != nil || next["action"] != "extract" || !strings.Contains(next["content"].(string), "open source-criticism") {
		t.Fatalf("a second immutable session event must remain extractable: next=%v err=%v", next, err)
	}
}
