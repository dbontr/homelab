package store

import (
	"sort"
	"strings"
)

// ReasoningFormalization declares an explicit propositional interpretation of
// source-attributed statements. Atom definitions are assertions supplied by the
// caller, not verified translations of natural language.
type ReasoningFormalization struct {
	Premises   map[string]string `json:"premises"`
	Conclusion string            `json:"conclusion"`
	Atoms      map[string]string `json:"atoms"`
	Scope      string            `json:"scope"`
}

func formalClaimSnapshot(db *database, chain row) (string, []string) {
	premises := parseList(chain["premises_json"])
	insist(len(premises) > 0 && len(premises) <= 16 && stringsUnique(premises), "invalid formal reasoning premise set")
	conclusion := strVal(chain["conclusion_id"])
	insist(conclusion != "" && !set(premises)[conclusion], "invalid reasoning conclusion identity")
	sortedPremises := append([]string(nil), premises...)
	sort.Strings(sortedPremises)
	parts := []string{
		strVal(chain["id"]), strVal(chain["identity_hash"]),
		strVal(chain["relation"]), conclusion, js(sortedPremises),
	}
	for _, id := range append([]string{conclusion}, sortedPremises...) {
		p := ensureRow(getRow(db, "SELECT id,statement,context,domain,identity_hash FROM propositions WHERE id=?", id), "proposition", id)
		parts = append(parts, strVal(p["id"]), strVal(p["statement"]), strVal(p["context"]), strVal(p["domain"]), strVal(p["identity_hash"]))
	}
	return fingerprint(parts...), sortedPremises
}

func formalCheckPublic(db *database, chain row, record row) Value {
	var submitted ReasoningFormalization
	insist(jsonUnmarshal(strVal(record["input_json"]), &submitted) == nil, "invalid stored formalization")
	var report Value
	insist(jsonUnmarshal(strVal(record["result_json"]), &report) == nil, "invalid stored formal check result")
	current, _ := formalClaimSnapshot(db, chain)
	out := Value{
		"id":                     record["id"],
		"chain_id":               record["chain_id"],
		"created_at":             record["created_at"],
		"claim_snapshot_current": current == strVal(record["claim_snapshot_hash"]),
		"formalization":          submitted,
		"result":                 report,
	}
	if review := latestTranslationReview(db, chain, strVal(record["id"])); review != nil {
		out["latest_translation_review"] = review
	}
	return out
}

func latestFormalCheck(db *database, chain row) Value {
	record := getRow(db, "SELECT * FROM reasoning_formal_checks WHERE chain_id=? ORDER BY rowid DESC LIMIT 1", chain["id"])
	if record == nil {
		return nil
	}
	out := formalCheckPublic(db, chain, record)
	report := out["result"].(Value)
	summary := Value{
		"id":                                    out["id"],
		"verdict":                               report["verdict"],
		"claim_snapshot_current":                out["claim_snapshot_current"],
		"natural_language_entailment_certified": false,
	}
	if review := out["latest_translation_review"]; review != nil {
		summary["latest_translation_review"] = review
	}
	return summary
}

// CheckReasoningEntailment exhaustively checks one submitted formalization and
// retains the result. The store does not infer the interpretation of a source.
func (s *Store) CheckReasoningEntailment(chainID string, submitted ReasoningFormalization) (Value, error) {
	return s.with(func(db *database) Value {
		chain := ensureRow(getRow(db, "SELECT * FROM reasoning_chains WHERE id=?", chainID), "reasoning chain", chainID)
		insist(strVal(chain["relation"]) == "entails", "formal entailment is only applicable to an entails reasoning chain")
		snapshot, ids := formalClaimSnapshot(db, chain)
		insist(len(submitted.Premises) == len(ids), "formalization must include every stored premise exactly once")
		formulas := make([]string, 0, len(ids))
		for _, id := range ids {
			formula, exists := submitted.Premises[id]
			insist(exists, "missing formalization for premise "+id)
			formulas = append(formulas, formula)
		}
		submitted.Scope = strings.TrimSpace(submitted.Scope)
		insist(submitted.Scope != "" && len(submitted.Scope) <= 2000, "formalization scope must contain 1..2000 bytes")
		proof, err := checkPropositional(formulas, submitted.Conclusion, submitted.Atoms)
		if err != nil {
			panic(err)
		}
		redundant := []string{}
		for _, index := range proof["redundant_premise_indexes"].([]int) {
			redundant = append(redundant, ids[index])
		}
		delete(proof, "redundant_premise_indexes")
		proof["individually_redundant_premise_ids"] = redundant
		proof["premise_ids"] = ids
		proof["conclusion_id"] = chain["conclusion_id"]
		proof["scope"] = submitted.Scope
		requestHash := fingerprint(snapshot, js(submitted))
		if prior := getRow(db, "SELECT * FROM reasoning_formal_checks WHERE chain_id=? AND submission_hash=?", chainID, requestHash); prior != nil {
			out := formalCheckPublic(db, chain, prior)
			out["duplicate"] = true
			return out
		}
		id := uuid()
		exec(db, `INSERT INTO reasoning_formal_checks
			(id,chain_id,submission_hash,claim_snapshot_hash,input_json,result_json,created_at)
			VALUES(?,?,?,?,?,?,?)`, id, chainID, requestHash, snapshot, js(submitted), js(proof), now())
		record := ensureRow(getRow(db, "SELECT * FROM reasoning_formal_checks WHERE id=?", id), "formal check", id)
		out := formalCheckPublic(db, chain, record)
		out["duplicate"] = false
		return out
	})
}

func (s *Store) GetReasoningFormalChecks(chainID string, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit > 0 && limit <= 20, "limit must be 1..20")
		chain := ensureRow(getRow(db, "SELECT * FROM reasoning_chains WHERE id=?", chainID), "reasoning chain", chainID)
		checks := []Value{}
		for _, record := range getRows(db, "SELECT * FROM reasoning_formal_checks WHERE chain_id=? ORDER BY rowid DESC LIMIT ?", chainID, limit) {
			checks = append(checks, formalCheckPublic(db, chain, record))
		}
		return Value{
			"chain_id":                              chainID,
			"checks":                                checks,
			"natural_language_entailment_certified": false,
			"mastery_separate":                      true,
		}
	})
}
