package store

import (
	"time"
)

// A check records the method and artifact used to examine an actual learner
// solution. It is not a built-in proof assistant or independent certification.
func (s *Store) RecordProblemCheck(problemID, method, outcome, reference, notes string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(method, "model", "symbolic", "formal", "executable", "experimental", "expert"), "invalid verification method")
		insist(valid(outcome, "pass", "fail", "inconclusive"), "invalid verification outcome")
		reference = mustString(reference, "evidence_reference")
		notes = mustString(notes, "notes")
		ensureRow(getRow(db, "SELECT 1 FROM problems WHERE id=?", problemID), "problem", problemID)
		draft := ensureRow(getRow(db, "SELECT id FROM problem_drafts WHERE problem_id=? ORDER BY rowid DESC LIMIT 1", problemID), "draft", problemID)
		id := uuid()
		exec(db, "INSERT INTO problem_checks VALUES(?,?,?,?,?,?,?,?)", id, problemID, draft["id"], method, outcome, reference, notes, now())
		return Value{"check_id": id, "problem_id": problemID, "draft_id": draft["id"], "method": method, "outcome": outcome, "evidence_reference": reference, "notes": notes, "provisional_assertion": true, "authenticated_verifier": false}
	})
}

func masteryPortfolio(db *database, id string) Value {
	ensureRow(getRow(db, "SELECT id FROM concepts WHERE id=?", id), "concept", id)
	evidence := mapRows(getRows(db, "SELECT dimension,passed,confidence,assisted,recorded_at FROM evidence WHERE concept_id=? ORDER BY recorded_at DESC LIMIT 100", id))
	problems := []Value{}
	seen := map[string]bool{}
	varied := map[string]bool{}
	substantial := 0
	externallyChecked := 0
	conflictingChecks := 0
	for _, r := range getRows(db, `SELECT p.id,p.title,p.statement,p.status,p.kind,p.difficulty,p.mode,p.independently_solved,p.completed_at,pc.credited,
         pi.cue_class,pi.method AS practice_method FROM problems p JOIN problem_concepts pc ON pc.problem_id=p.id
         LEFT JOIN practice_items pi ON pi.problem_id=p.id WHERE pc.concept_id=? ORDER BY p.created_at DESC,p.id DESC LIMIT 100`, id) {
		identifier := strVal(r["id"])
		checks := getRows(db, `SELECT pc.method,pc.outcome,pc.evidence_reference,pc.checked_at,pc.draft_id,vc.verifier_id
            FROM problem_checks pc LEFT JOIN verified_problem_checks vc ON vc.check_id=pc.id
            WHERE pc.problem_id=? ORDER BY pc.rowid DESC`, identifier)
		lastDraft := getRow(db, "SELECT id FROM problem_drafts WHERE problem_id=? ORDER BY rowid DESC LIMIT 1", identifier)
		passedCheck, failedCheck := false, false
		for _, check := range checks {
			if lastDraft == nil || check["draft_id"] != lastDraft["id"] || strVal(check["verifier_id"]) == "" {
				continue
			}
			switch strVal(check["outcome"]) {
			case "pass":
				passedCheck = true
			case "fail":
				failedCheck = true
			}
		}
		checked := passedCheck && !failedCheck
		conflict := passedCheck && failedCheck
		if conflict {
			conflictingChecks++
		}
		verifiedIndependent := boolVal(r["independently_solved"]) && boolVal(r["credited"]) && strVal(r["status"]) == "solved"
		fresh := false
		if verifiedIndependent && valid(strVal(r["difficulty"]), "challenging", "research") {
			key := fingerprint(canonical(strVal(r["statement"])))
			if !seen[key] {
				seen[key] = true
				fresh = true
				substantial++
				if checked {
					externallyChecked++
				}
				kind := strVal(r["kind"])
				cue := strVal(r["cue_class"])
				varied[kind+"/"+cue] = true
			}
		}
		problems = append(problems, Value{"id": identifier, "title": r["title"], "kind": r["kind"], "difficulty": r["difficulty"], "independently_solved": verifiedIndependent,
			"fresh_substantial_solution": fresh, "checks": mapRows(checks), "external_check_recorded": checked, "verification_conflict": conflict, "practice_method": r["practice_method"], "cue_class": r["cue_class"]})
	}
	floor := levelOnly(db, id)
	pass := floor >= 5 && substantial >= 3 && len(varied) >= 2 && externallyChecked >= 1 && conflictingChecks == 0
	obstacles := []string{}
	if floor < 5 {
		obstacles = append(obstacles, "Missing one or more foundational conceptual, reconstruction, transfer or delayed-retention assessments")
	}
	if substantial < 3 {
		obstacles = append(obstacles, "Fewer than three distinct independently solved, challenging or research-level problems")
	}
	if len(varied) < 2 {
		obstacles = append(obstacles, "Insufficient variety across problem kinds and cue classes")
	}
	if externallyChecked < 1 {
		obstacles = append(obstacles, "No independently corroborated solution check recorded")
	}
	if conflictingChecks > 0 {
		obstacles = append(obstacles, "A latest draft has conflicting authenticated verifier outcomes")
	}
	// Authenticated checker receipts establish origin, not that the checker is sound
	// or that the learner has mastered every possible application.
	return Value{"concept_id": id, "foundational_level": floor, "distinct_difficult_solutions": substantial, "varied_problem_classes": len(varied),
		"externally_checked_solutions": externallyChecked, "conflicting_checker_reports": conflictingChecks, "full_mastery_evidence_recorded": pass, "unresolved": obstacles, "problems": problems, "conceptual_evidence": evidence,
		"assessment_limitations": "Only authenticated checker receipts count as external corroboration; any contradictory signed pass/fail reports block full mastery. Signatures authenticate a report, not verifier correctness or coverage; model judgments remain provisional."}
}
func (s *Store) MasteryPortfolio(id string) (Value, error) {
	return s.with(func(db *database) Value { return masteryPortfolio(db, id) })
}
func (s *Store) DueReassessments(limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 100, "limit must be 1..100")
		before := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02T15:04:05.000000+00:00")
		rows := getRows(db, `SELECT c.id,c.title,e.recorded_at AS last_reconstruction FROM concepts c
            JOIN evidence e ON e.concept_id=c.id AND e.dimension='independent_reconstruction'
            WHERE e.passed=1 AND e.assisted=0 AND e.confidence>=?
              AND e.recorded_at<=?
              AND e.rowid=(SELECT latest.rowid FROM evidence latest
                  WHERE latest.concept_id=c.id AND latest.dimension='independent_reconstruction'
                  ORDER BY latest.recorded_at DESC,latest.rowid DESC LIMIT 1)
              AND NOT EXISTS (
                  SELECT 1 FROM evidence er WHERE er.concept_id=c.id AND er.dimension='retention'
                    AND er.rowid=(SELECT recent.rowid FROM evidence recent
                        WHERE recent.concept_id=c.id AND recent.dimension='retention'
                        ORDER BY recent.recorded_at DESC,recent.rowid DESC LIMIT 1)
                    AND er.passed=1 AND er.assisted=0 AND er.confidence>=?
                    AND julianday(er.recorded_at)-julianday(e.recorded_at)>=1
              ) ORDER BY e.recorded_at,c.id LIMIT ?`, minConfidence, before, minConfidence, limit)
		return Value{"due": mapRows(rows), "minimum_delay_hours": 24}
	})
}
