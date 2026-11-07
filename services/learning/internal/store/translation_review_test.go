package store

import (
	"strings"
	"testing"
)

func translationFixture(t *testing.T) (*Store, string, string, TranslationReview) {
	t.Helper()
	s, propositions, chainID := deductiveChainFixture(t)
	form := ReasoningFormalization{
		Premises: map[string]string{
			propositions["If the switch is closed, the lamp is lit"]: "P -> Q",
			propositions["The switch is closed"]:                     "P",
		},
		Conclusion: "Q",
		Atoms:      map[string]string{"P": "The switch is closed", "Q": "The lamp is lit"},
		Scope:      "Ideal binary circuit",
	}
	check, err := s.CheckReasoningEntailment(chainID, form)
	if err != nil {
		t.Fatal(err)
	}
	anchors := []TranslationAnchor{}
	for _, id := range []string{
		propositions["If the switch is closed, the lamp is lit"],
		propositions["The switch is closed"],
		propositions["The lamp is lit"],
	} {
		source, e := s.GetProposition(id)
		if e != nil {
			t.Fatal(e)
		}
		proofs := source["provenance"].([]Value)
		if len(proofs) != 1 {
			t.Fatal("expected one accepted source quotation", proofs)
		}
		anchors = append(anchors, TranslationAnchor{
			PropositionID: id,
			SourceSpanID:  proofs[0]["source_span_id"].(string),
			QuotedText:    proofs[0]["quoted_text"].(string),
			Alignment:     "aligned",
			Explanation:   "This quotation is the statement being abstracted by the corresponding formula.",
		})
	}
	review := TranslationReview{
		Decision:    "aligned",
		ReviewerRef: "review:source-examination-1",
		Reason:      "The supplied meaning of P and Q matches the cited binary circuit claims in this scope.",
		Domain:      "propositional",
		Anchors:     anchors,
	}
	return s, check["id"].(string), chainID, review
}

func TestTranslationReviewBindsEveryClaimToOriginalCitation(t *testing.T) {
	s, checkID, chainID, review := translationFixture(t)
	saved, err := s.RecordTranslationReview(checkID, review)
	if err != nil {
		t.Fatal(err)
	}
	if saved["reviewer_authenticated"] != false || saved["translation_verified"] != false ||
		saved["claim_snapshot_current"] != true || saved["source_evidence_current"] != true ||
		saved["mastery_unchanged"] != true {
		t.Fatal("source-anchored review must not be represented as an independent certificate", saved)
	}
	repeated, err := s.RecordTranslationReview(checkID, review)
	if err != nil || repeated["id"] != saved["id"] || repeated["duplicate"] != true {
		t.Fatal("identical review submissions must be idempotent", repeated, err)
	}
	reversed := review
	reversed.Anchors = append([]TranslationAnchor(nil), review.Anchors...)
	for i, j := 0, len(reversed.Anchors)-1; i < j; i, j = i+1, j-1 {
		reversed.Anchors[i], reversed.Anchors[j] = reversed.Anchors[j], reversed.Anchors[i]
	}
	canonical, err := s.RecordTranslationReview(checkID, reversed)
	if err != nil || canonical["id"] != saved["id"] {
		t.Fatal("order changes must not create duplicate review records", canonical, err)
	}
	history, err := s.GetTranslationReviews(checkID, 5)
	if err != nil || len(history["reviews"].([]Value)) != 1 {
		t.Fatal("review history must preserve one canonical submission", history, err)
	}
	checks, err := s.GetReasoningFormalChecks(chainID, 10)
	if err != nil {
		t.Fatal(err)
	}
	latest := checks["checks"].([]Value)[0]["latest_translation_review"].(Value)
	if latest["decision"] != "aligned" || latest["translation_verified"] != false {
		t.Fatal("formal check must expose review without claiming independent verification", latest)
	}
	graph, err := s.ReasoningGraph(15)
	if err != nil {
		t.Fatal(err)
	}
	if graph["chains"].([]Value)[0]["latest_formal_check"].(Value)["latest_translation_review"].(Value)["decision"] != "aligned" {
		t.Fatal("graph must expose translation review without changing mastery or chain status", graph)
	}
}

func TestTranslationReviewRejectsIncompleteAndFabricatedEvidence(t *testing.T) {
	s, checkID, _, baseline := translationFixture(t)
	checkRejects := func(label string, review TranslationReview) {
		t.Helper()
		if _, err := s.RecordTranslationReview(checkID, review); err == nil {
			t.Fatal("invalid translation review accepted: ", label)
		}
	}
	cases := map[string]func(*TranslationReview){
		"missing conclusion":             func(v *TranslationReview) { v.Anchors = v.Anchors[:2] },
		"duplicate premise":              func(v *TranslationReview) { v.Anchors[2] = v.Anchors[0] },
		"fabricated quote":               func(v *TranslationReview) { v.Anchors[0].QuotedText = "Fabricated citation" },
		"mismatched span":                func(v *TranslationReview) { v.Anchors[0].SourceSpanID = "not-a-real-source" },
		"contradictory verdict":          func(v *TranslationReview) { v.Anchors[0].Alignment = "misaligned" },
		"empty reviewer reference":       func(v *TranslationReview) { v.ReviewerRef = "  " },
		"unexplained review":             func(v *TranslationReview) { v.Anchors[0].Explanation = "" },
		"unsupported domain":             func(v *TranslationReview) { v.Domain = "quantum_certified" },
		"quantifier without limitations": func(v *TranslationReview) { v.Domain = "quantified" },
		"empirical without assumptions": func(v *TranslationReview) {
			v.Domain = "empirical"
			v.Limitations = []string{"Observed data are not a deductive guarantee"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			probe := baseline
			probe.Anchors = append([]TranslationAnchor(nil), baseline.Anchors...)
			mutate(&probe)
			checkRejects(name, probe)
		})
	}
	needsReview := baseline
	needsReview.Anchors = append([]TranslationAnchor(nil), baseline.Anchors...)
	needsReview.Anchors[0].Alignment = "uncertain"
	needsReview.Decision = "uncertain"
	needsReview.Domain = "quantified"
	needsReview.Limitations = []string{"Quantifiers and domain size are not modeled by the propositional checker"}
	uncertain, err := s.RecordTranslationReview(checkID, needsReview)
	if err != nil || uncertain["domain_validation_path"] != "quantifier_aware_proof_required" ||
		uncertain["translation_verified"] != false {
		t.Fatal("nonpropositional review must retain its validation ceiling", uncertain, err)
	}
}

func TestTranslationReviewPreservesDissentAndSourceStaleness(t *testing.T) {
	s, checkID, chainID, review := translationFixture(t)
	initial, err := s.RecordTranslationReview(checkID, review)
	if err != nil {
		t.Fatal(err)
	}
	dissent := review
	dissent.Decision = "misaligned"
	dissent.Reason = "The quoted circuit statement may assume an ideal lamp."
	dissent.ReviewerRef = "review:source-examination-2"
	dissent.Anchors = append([]TranslationAnchor(nil), review.Anchors...)
	dissent.Anchors[0].Alignment = "misaligned"
	dissent.Anchors[0].Explanation = "The conditional omits the possibility of a broken lamp."
	if _, err := s.RecordTranslationReview(checkID, dissent); err != nil {
		t.Fatal(err)
	}
	all, err := s.GetTranslationReviews(checkID, 10)
	if err != nil || len(all["reviews"].([]Value)) != 2 {
		t.Fatal("conflicting review judgments must both remain accessible", all, err)
	}
	checks, err := s.GetReasoningFormalChecks(chainID, 5)
	if err != nil {
		t.Fatal(err)
	}
	latest := checks["checks"].([]Value)[0]["latest_translation_review"].(Value)
	if latest["decision"] != "misaligned" {
		t.Fatal("latest review must not overwrite previous recorded judgments", latest)
	}
	anchor := review.Anchors[0]
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE source_spans SET content=content || ? WHERE id=?", " altered", anchor.SourceSpanID)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	all, err = s.GetTranslationReviews(checkID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range all["reviews"].([]Value) {
		if record["source_evidence_current"] != false {
			t.Fatal("altered source must invalidate every anchored review", record)
		}
	}
	if _, err := s.RecordTranslationReview(checkID, review); err == nil {
		t.Fatal("altered source must not permit a fresh review record")
	}
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE source_spans SET content=substr(content,1,length(content)-?) WHERE id=?", len(" altered"), anchor.SourceSpanID)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	all, err = s.GetTranslationReviews(checkID, 10)
	if err != nil || all["reviews"].([]Value)[0]["source_evidence_current"] != true {
		t.Fatal("restoring the original span should restore its recorded evidence binding", all, err)
	}
	propositionID := anchor.PropositionID
	if _, err := s.with(func(db *database) Value {
		exec(db, "UPDATE propositions SET statement=? WHERE id=?", "Altered proposition statement", propositionID)
		return Value{}
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := s.GetTranslationReviews(checkID, 10)
	if err != nil || stale["reviews"].([]Value)[0]["claim_snapshot_current"] != false {
		t.Fatal("changed proposition wording must mark historical reviews stale", stale, err)
	}
	if _, err := s.RecordTranslationReview(checkID, dissent); err == nil {
		t.Fatal("cannot bind a fresh review to an obsolete formal check")
	}
	if initial["id"] == nil {
		t.Fatal("audit identifier must be durable")
	}
}

func TestTranslationReviewPersistsWithoutSelfAuthentication(t *testing.T) {
	s, checkID, _, review := translationFixture(t)
	if _, err := s.RecordTranslationReview(checkID, review); err != nil {
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
	all, err := reopened.GetTranslationReviews(checkID, 5)
	if err != nil || len(all["reviews"].([]Value)) != 1 {
		t.Fatal("review history must survive restart", all, err)
	}
	input := all["reviews"].([]Value)[0]["review"].(TranslationReview)
	if len(input.Anchors) != 3 || !strings.Contains(input.Reason, "binary circuit") ||
		all["reviewer_authenticated"] != false || all["translation_verified"] != false {
		t.Fatal("review must retain provenance without asserting authenticated independent assessment", all)
	}
}
