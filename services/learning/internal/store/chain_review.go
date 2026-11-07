package store

import (
	"encoding/json"
	"sort"
	"strings"
)

// chainReviewBlockers evaluates the current evidence for every premise and the
// conclusion. A stored review is historical; later disputes or damaged
// provenance must still prevent a chain from appearing review-supported.
func chainReviewBlockers(db *database, chain row) []string {
	var premises []string
	if err := json.Unmarshal([]byte(strVal(chain["premises_json"])), &premises); err != nil || len(premises) == 0 || len(premises) > 16 {
		return []string{"invalid_premise_set"}
	}
	conclusion := strVal(chain["conclusion_id"])
	ids := append([]string{conclusion}, premises...)
	inChain := map[string]bool{}
	for _, id := range ids {
		if id == "" || inChain[id] {
			return []string{"invalid_premise_set"}
		}
		inChain[id] = true
	}
	ordered := append([]string(nil), premises...)
	sort.Strings(ordered)
	if strVal(chain["identity_hash"]) != fingerprint(conclusion, js(ordered), strVal(chain["relation"])) {
		return []string{"invalid_chain_identity"}
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	claims := getRows(db, `SELECT p.id, p.status, pv.quoted_text, sp.content AS span_content,
		sp.fingerprint AS span_fingerprint, b.status AS batch_status,
		b.fingerprint AS batch_fingerprint, b.span_id AS batch_span_id, sp.id AS span_id
		FROM propositions p
		LEFT JOIN proposition_provenance pv ON pv.proposition_id=p.id
		LEFT JOIN source_spans sp ON sp.id=pv.source_span_id
		LEFT JOIN extraction_batches b ON b.id=pv.batch_id
		WHERE p.id IN (`+placeholders+`)`, args...)

	found := map[string]bool{}
	hadProvenance := map[string]bool{}
	invalidProvenance := map[string]bool{}
	notReviewed := map[string]bool{}
	for _, claim := range claims {
		id := strVal(claim["id"])
		found[id] = true
		if strVal(claim["status"]) != "reviewed" {
			notReviewed[id] = true
		}
		if claim["quoted_text"] == nil {
			continue
		}
		hadProvenance[id] = true
		quote := strVal(claim["quoted_text"])
		span := strVal(claim["span_content"])
		if quote == "" || !strings.Contains(span, quote) ||
			strVal(claim["span_fingerprint"]) != fingerprint(span) ||
			strVal(claim["batch_status"]) != "accepted" ||
			strVal(claim["batch_span_id"]) != strVal(claim["span_id"]) ||
			strVal(claim["batch_fingerprint"]) != strVal(claim["span_fingerprint"]) {
			invalidProvenance[id] = true
		}
	}

	blockers := []string{}
	for _, id := range ids {
		switch {
		case !found[id]:
			blockers = append(blockers, "missing_claim:"+id)
		case notReviewed[id]:
			blockers = append(blockers, "claim_not_reviewed:"+id)
		}
		if !hadProvenance[id] {
			blockers = append(blockers, "missing_claim_provenance:"+id)
		} else if invalidProvenance[id] {
			blockers = append(blockers, "invalid_claim_provenance:"+id)
		}
	}

	chainProofs := getRows(db, `SELECT cp.source_span_id, sp.content, sp.fingerprint AS span_fingerprint,
		b.status AS batch_status, b.fingerprint AS batch_fingerprint, b.span_id AS batch_span_id
		FROM chain_provenance cp
		JOIN source_spans sp ON sp.id=cp.source_span_id
		JOIN extraction_batches b ON b.id=cp.batch_id
		WHERE cp.chain_id=?`, chain["id"])
	if len(chainProofs) == 0 {
		blockers = append(blockers, "missing_chain_provenance")
	}
	for _, proof := range chainProofs {
		if strVal(proof["batch_status"]) != "accepted" ||
			strVal(proof["batch_span_id"]) != strVal(proof["source_span_id"]) ||
			strVal(proof["batch_fingerprint"]) != strVal(proof["span_fingerprint"]) ||
			fingerprint(strVal(proof["content"])) != strVal(proof["span_fingerprint"]) {
			blockers = append(blockers, "invalid_chain_provenance")
			break
		}
	}

	disputeArgs := append(append([]any{}, args...), args...)
	disputes := getRows(db, `SELECT id,left_id,right_id,status FROM proposition_disputes
		WHERE left_id IN (`+placeholders+`) OR right_id IN (`+placeholders+`)`, disputeArgs...)
	for _, d := range disputes {
		left, right := strVal(d["left_id"]), strVal(d["right_id"])
		blocks := false
		switch strVal(d["status"]) {
		case "unresolved":
			blocks = inChain[left] || inChain[right]
		case "left_supported":
			blocks = inChain[right]
		case "right_supported":
			blocks = inChain[left]
		case "both_unsupported":
			blocks = inChain[left] || inChain[right]
		case "different_contexts":
			// Different scopes may both be defensible, but cannot be combined
			// into one undifferentiated argument.
			blocks = inChain[left] && inChain[right]
		}
		if blocks {
			blockers = append(blockers, "dispute:"+strVal(d["id"])+":"+strVal(d["status"]))
		}
	}
	sort.Strings(blockers)
	return blockers
}

func chainReviewStanding(db *database, chain row) Value {
	blockers := chainReviewBlockers(db, chain)
	return Value{
		"review_eligible": len(blockers) == 0,
		"review_active":   strVal(chain["status"]) == "reviewed" && len(blockers) == 0,
		"review_blockers": blockers,
	}
}

// GetReasoningChain returns a complete, joint-premise inference with its
// original provenance and append-only review decisions.
func (s *Store) GetReasoningChain(id string) (Value, error) {
	return s.with(func(db *database) Value {
		r := ensureRow(getRow(db, "SELECT * FROM reasoning_chains WHERE id=?", id), "reasoning chain", id)
		for k, v := range chainReviewStanding(db, r) {
			r[k] = v
		}
		provenance := mapRows(getRows(db, `SELECT k.id AS source_id,k.label AS source_name,
			sp.id AS span_id,sp.ordinal,cp.batch_id
			FROM chain_provenance cp JOIN source_spans sp ON sp.id=cp.source_span_id
			JOIN knowledge_sources k ON k.id=sp.source_id
			WHERE cp.chain_id=? ORDER BY k.id,sp.ordinal`, id))
		history := mapRows(getRows(db, `SELECT id,status,explanation,recorded_at
			FROM reasoning_chain_review_history WHERE chain_id=?
			ORDER BY recorded_at,id`, id))
		checks := []Value{}
		for _, record := range getRows(db, "SELECT * FROM reasoning_formal_checks WHERE chain_id=? ORDER BY rowid DESC LIMIT 10", id) {
			checks = append(checks, formalCheckPublic(db, r, record))
		}
		return Value{
			"chain":            Value(r),
			"premises":         parseList(r["premises_json"]),
			"provenance":       provenance,
			"review_history":   history,
			"formal_checks":    checks,
			"mastery_separate": true,
		}
	})
}
