package store

import (
	"sort"
	"strings"
)

// TranslationAnchor records a source-grounded judgment about one formalized
// proposition. The judgment is not independently authenticated.
type TranslationAnchor struct {
	PropositionID string `json:"proposition_id"`
	SourceSpanID  string `json:"source_span_id"`
	QuotedText    string `json:"quoted_text"`
	Alignment     string `json:"alignment"`
	Explanation   string `json:"explanation"`
}

// TranslationReview preserves the reviewer's claims about the correspondence
// between source language and a specific propositional formalization.
type TranslationReview struct {
	Decision    string              `json:"decision"`
	ReviewerRef string              `json:"reviewer_ref"`
	Reason      string              `json:"reason"`
	Domain      string              `json:"domain"`
	Assumptions []string            `json:"assumptions"`
	Limitations []string            `json:"limitations"`
	Anchors     []TranslationAnchor `json:"anchors"`
}

func translationValidationPath(domain string) string {
	switch domain {
	case "propositional":
		return "independent_translation_authentication_required"
	case "quantified":
		return "quantifier_aware_proof_required"
	case "mathematical":
		return "domain_specific_mathematical_verification_required"
	case "empirical":
		return "measurement_and_uncertainty_validation_required"
	case "model_dependent":
		return "model_assumption_and_applicability_validation_required"
	case "historical":
		return "independent_source_criticism_required"
	}
	return "review_required"
}

func validateTranslationNotes(values []string, name string) {
	insist(len(values) <= 16, name+" allows at most 16 entries")
	for _, value := range values {
		insist(strings.TrimSpace(value) == value && len(value) > 0 && len(value) <= 1200, name+" entries must contain 1..1200 trimmed bytes")
	}
}

// translationSourceCurrent checks the exact provenance tuple and the original
// source bytes; the quote alone is insufficient to establish provenance.
func translationSourceCurrent(db *database, anchor TranslationAnchor) bool {
	matched := getRow(db, `SELECT b.id AS batch_id, b.status AS batch_status,
		b.span_id AS batch_span_id, b.fingerprint AS batch_fingerprint,
		sp.id, sp.source_id, sp.ordinal, sp.content, sp.fingerprint, sp.processed_at
		FROM proposition_provenance pv
		JOIN source_spans sp ON sp.id=pv.source_span_id
		JOIN extraction_batches b ON b.id=pv.batch_id
		WHERE pv.proposition_id=? AND pv.source_span_id=? AND pv.quoted_text=?`,
		anchor.PropositionID, anchor.SourceSpanID, anchor.QuotedText)
	return matched != nil && strVal(matched["batch_status"]) == "accepted" &&
		strVal(matched["batch_span_id"]) == strVal(matched["id"]) &&
		strVal(matched["batch_fingerprint"]) == strVal(matched["fingerprint"]) &&
		strings.Contains(strVal(matched["content"]), anchor.QuotedText) &&
		sourceSpanIntact(db, matched)
}

func translationReviewPublic(db *database, chain row, record row) Value {
	var input TranslationReview
	insist(jsonUnmarshal(strVal(record["review_json"]), &input) == nil, "invalid stored translation review")
	currentSnapshot, _ := formalClaimSnapshot(db, chain)
	sourceCurrent := true
	for _, anchor := range input.Anchors {
		if !translationSourceCurrent(db, anchor) {
			sourceCurrent = false
		}
	}
	return Value{
		"id":                          record["id"],
		"check_id":                    record["check_id"],
		"chain_id":                    record["chain_id"],
		"recorded_at":                 record["recorded_at"],
		"review":                      input,
		"claim_snapshot_current":      currentSnapshot == strVal(record["claim_snapshot_hash"]),
		"source_evidence_current":     sourceCurrent,
		"reviewer_authenticated":      false,
		"translation_verified":        false,
		"semantic_validity_certified": false,
		"domain_validation_path":      translationValidationPath(input.Domain),
		"mastery_unchanged":           true,
	}
}

// RecordTranslationReview creates an append-only, source-anchored audit record
// for one exact formal check. A label supplied by the host is not an identity
// credential, and recording a positive review does not certify correctness.
func (s *Store) RecordTranslationReview(checkID string, input TranslationReview) (Value, error) {
	return s.with(func(db *database) Value {
		check := ensureRow(getRow(db, "SELECT * FROM reasoning_formal_checks WHERE id=?", checkID), "formal check", checkID)
		chain := ensureRow(getRow(db, "SELECT * FROM reasoning_chains WHERE id=?", check["chain_id"]), "reasoning chain", strVal(check["chain_id"]))
		snapshot, premises := formalClaimSnapshot(db, chain)
		insist(snapshot == strVal(check["claim_snapshot_hash"]), "formalization is stale; create a fresh check")
		insist(valid(input.Decision, "aligned", "misaligned", "uncertain"), "invalid translation review decision")
		insist(valid(input.Domain, "propositional", "quantified", "mathematical", "empirical", "model_dependent", "historical"), "invalid reasoning domain")
		input.ReviewerRef = strings.TrimSpace(input.ReviewerRef)
		input.Reason = strings.TrimSpace(input.Reason)
		insist(len(input.ReviewerRef) > 0 && len(input.ReviewerRef) <= 160, "reviewer reference must be 1..160 bytes")
		insist(len(input.Reason) > 0 && len(input.Reason) <= 2500, "review rationale must be 1..2500 bytes")
		validateTranslationNotes(input.Assumptions, "assumptions")
		validateTranslationNotes(input.Limitations, "limitations")
		if input.Domain != "propositional" {
			insist(len(input.Limitations) > 0, "non-propositional reasoning requires a statement of validation limits")
		}
		if input.Domain == "empirical" || input.Domain == "model_dependent" {
			insist(len(input.Assumptions) > 0, "empirical and model-dependent reasoning require explicit assumptions")
		}

		var formal ReasoningFormalization
		insist(jsonUnmarshal(strVal(check["input_json"]), &formal) == nil, "invalid stored formalization")
		expected := set(append(append([]string(nil), premises...), strVal(chain["conclusion_id"])))
		insist(len(input.Anchors) == len(expected), "review must address every premise and the conclusion exactly once")
		seen := map[string]bool{}
		bad, uncertain := false, false
		for _, anchor := range input.Anchors {
			id := anchor.PropositionID
			insist(expected[id] && !seen[id], "unknown or duplicated proposition in translation review")
			seen[id] = true
			insist(valid(anchor.Alignment, "aligned", "misaligned", "uncertain"), "invalid proposition alignment")
			insist(len(anchor.Explanation) > 0 && len(anchor.Explanation) <= 1500 && strings.TrimSpace(anchor.Explanation) == anchor.Explanation, "each proposition requires a concise review explanation")
			insist(len(anchor.QuotedText) > 0 && len(anchor.QuotedText) <= 4096 && len(anchor.SourceSpanID) > 0, "source quotation and span ID required")
			insist(translationSourceCurrent(db, anchor), "quoted source evidence is missing or damaged for "+id)
			if anchor.Alignment == "misaligned" {
				bad = true
			}
			if anchor.Alignment == "uncertain" {
				uncertain = true
			}
		}
		if input.Decision == "aligned" {
			insist(!bad && !uncertain, "an aligned review requires alignment of every proposition")
		} else if input.Decision == "misaligned" {
			insist(bad, "a misaligned review must identify a misaligned proposition")
		} else {
			insist(uncertain || bad, "an uncertain review must identify a disputed mapping")
		}
		sort.Slice(input.Anchors, func(i, j int) bool {
			return input.Anchors[i].PropositionID < input.Anchors[j].PropositionID
		})
		idempotency := fingerprint(strVal(check["chain_id"]), checkID, snapshot, js(input))
		if existing := getRow(db, "SELECT * FROM reasoning_translation_reviews WHERE check_id=? AND submission_hash=?", checkID, idempotency); existing != nil {
			out := translationReviewPublic(db, chain, existing)
			out["duplicate"] = true
			return out
		}
		id := uuid()
		exec(db, `INSERT INTO reasoning_translation_reviews
			(id,check_id,chain_id,submission_hash,claim_snapshot_hash,review_json,recorded_at)
			VALUES(?,?,?,?,?,?,?)`, id, checkID, check["chain_id"], idempotency, snapshot, js(input), now())
		record := ensureRow(getRow(db, "SELECT * FROM reasoning_translation_reviews WHERE id=?", id), "translation review", id)
		out := translationReviewPublic(db, chain, record)
		out["duplicate"] = false
		return out
	})
}

func (s *Store) GetTranslationReviews(checkID string, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 20, "limit must be 1..20")
		check := ensureRow(getRow(db, "SELECT * FROM reasoning_formal_checks WHERE id=?", checkID), "formal check", checkID)
		chain := ensureRow(getRow(db, "SELECT * FROM reasoning_chains WHERE id=?", check["chain_id"]), "reasoning chain", strVal(check["chain_id"]))
		reviews := []Value{}
		for _, record := range getRows(db, "SELECT * FROM reasoning_translation_reviews WHERE check_id=? ORDER BY rowid DESC LIMIT ?", checkID, limit) {
			reviews = append(reviews, translationReviewPublic(db, chain, record))
		}
		return Value{
			"check_id":               checkID,
			"reviews":                reviews,
			"reviewer_authenticated": false,
			"translation_verified":   false,
			"mastery_separate":       true,
		}
	})
}

func latestTranslationReview(db *database, chain row, checkID string) Value {
	record := getRow(db, "SELECT * FROM reasoning_translation_reviews WHERE check_id=? ORDER BY rowid DESC LIMIT 1", checkID)
	if record == nil {
		return nil
	}
	review := translationReviewPublic(db, chain, record)
	input := review["review"].(TranslationReview)
	return Value{
		"id":                      review["id"],
		"decision":                input.Decision,
		"domain":                  input.Domain,
		"claim_snapshot_current":  review["claim_snapshot_current"],
		"source_evidence_current": review["source_evidence_current"],
		"reviewer_authenticated":  false,
		"translation_verified":    false,
		"domain_validation_path":  review["domain_validation_path"],
	}
}
