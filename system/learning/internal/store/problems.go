package store

import (
	"math"
	"strings"
)

func problemPublic(db *database, id string, history bool) Value {
	r := ensureRow(getRow(db, "SELECT * FROM problems WHERE id=?", id), "problem", id)
	p := Value{}
	for k, v := range r {
		if k != "reference_solution" && k != "grading_rubric" {
			p[k] = v
		}
	}
	p["independently_solved"] = boolVal(p["independently_solved"])
	p["solution_available"] = strVal(p["status"]) != "open" && strVal(r["reference_solution"]) != ""
	ids := []string{}
	credits := []string{}
	for _, c := range getRows(db, "SELECT concept_id,credited FROM problem_concepts WHERE problem_id=? ORDER BY concept_id", id) {
		s := strVal(c["concept_id"])
		ids = append(ids, s)
		if boolVal(c["credited"]) {
			credits = append(credits, s)
		}
	}
	p["concept_ids"] = ids
	p["credited_concept_ids"] = credits
	if history {
		p["checks"] = mapRows(getRows(db, "SELECT id,draft_id,method,outcome,evidence_reference,notes,checked_at FROM problem_checks WHERE problem_id=? ORDER BY rowid", id))
		p["drafts"] = mapRows(getRows(db, `SELECT d.id,d.body,d.recorded_at,r.learner_reasoning,r.self_confidence FROM problem_drafts d
             LEFT JOIN problem_draft_reflections r ON r.draft_id=d.id WHERE d.problem_id=? ORDER BY d.rowid`, id))
		p["hints"] = mapRows(getRows(db, "SELECT id,level,guidance,recorded_at FROM problem_hints WHERE problem_id=? ORDER BY rowid", id))
		p["reviews"] = mapRows(getRows(db, "SELECT id,draft_id,verdict,feedback,recorded_at FROM problem_reviews WHERE problem_id=? ORDER BY rowid", id))
	}
	return p
}
func (s *Store) CreateProblem(title, statement, kind, mode, difficulty string, conceptIDs []string, discipline, origin, solution, rubric, source string) (Value, error) {
	return s.with(func(db *database) Value {
		return createProblem(db, title, statement, kind, mode, difficulty, conceptIDs, discipline, origin, solution, rubric, source)
	})
}
func createProblem(db *database, title, statement, kind, mode, difficulty string, conceptIDs []string, discipline, origin, solution, rubric, source string) Value {
	insist(valid(kind, "proof", "calculation", "derivation", "programming", "scientific", "other"), "invalid problem kind")
	insist(valid(mode, "independent", "guided", "exam"), "invalid problem mode")
	insist(valid(difficulty, "foundational", "standard", "challenging", "research"), "invalid problem difficulty")
	insist(valid(origin, "generated", "user"), "origin must be generated or user")
	insist(stringsUnique(conceptIDs), "concept_ids must be distinct concept identifiers")
	title = mustString(title, "title")
	statement = mustString(statement, "statement")
	if origin == "generated" {
		solution = mustString(solution, "reference_solution")
		rubric = mustString(rubric, "grading_rubric")
	}
	if solution != "" {
		solution = mustString(solution, "reference_solution")
	}
	if rubric != "" {
		rubric = mustString(rubric, "grading_rubric")
	}
	for _, cid := range conceptIDs {
		ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", cid), "concept", cid)
	}
	id := uuid()
	exec(db, `INSERT INTO problems(id,title,statement,discipline,kind,mode,difficulty,origin,source_reference,reference_solution,grading_rubric,status,independently_solved,created_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,'open',0,?)`, id, title, statement, strings.TrimSpace(discipline), kind, mode, difficulty, origin, strings.TrimSpace(source), solution, rubric, now())
	for _, cid := range conceptIDs {
		exec(db, "INSERT INTO problem_concepts(problem_id,concept_id) VALUES(?,?)", id, cid)
	}
	return problemPublic(db, id, true)
}
func (s *Store) GetProblem(id string) (Value, error) {
	return s.with(func(db *database) Value { return problemPublic(db, id, true) })
}
func (s *Store) ListProblems(status string, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(status, "open", "solved", "incomplete", "abandoned", "all"), "invalid problem status")
		insist(limit >= 1 && limit <= 100, "limit must be 1..100")
		q := "SELECT id FROM problems ORDER BY created_at DESC,id DESC LIMIT ?"
		args := []any{limit}
		if status != "all" {
			q = "SELECT id FROM problems WHERE status=? ORDER BY created_at DESC,id DESC LIMIT ?"
			args = []any{status, limit}
		}
		result := []Value{}
		for _, r := range getRows(db, q, args...) {
			result = append(result, problemPublic(db, strVal(r["id"]), false))
		}
		return Value{"problems": result, "filter": status}
	})
}
func (s *Store) SaveProblemDraft(id, draft string) (Value, error) {
	return s.SaveProblemDraftWithReflection(id, draft, "", nil)
}

// SaveProblemDraftWithReflection keeps the learner's explanation and confidence
// attached to the exact immutable revision. These are self-reports, not grades.
func (s *Store) SaveProblemDraftWithReflection(id, draft, reasoning string, selfConfidence *float64) (Value, error) {
	return s.with(func(db *database) Value {
		draft = mustString(draft, "draft")
		insist(len(reasoning) <= 20000, "learner reasoning is too long")
		if selfConfidence != nil {
			insist(!math.IsNaN(*selfConfidence) && !math.IsInf(*selfConfidence, 0) && *selfConfidence >= 0 && *selfConfidence <= 1, "self-confidence must be between 0 and 1")
		}
		p := problemPublic(db, id, false)
		insist(strVal(p["status"]) == "open", "cannot edit a closed problem")
		previous := getRow(db, "SELECT id,body FROM problem_drafts WHERE problem_id=? ORDER BY rowid DESC LIMIT 1", id)
		var draftID string
		duplicate := false
		if previous != nil && strVal(previous["body"]) == draft {
			prior := getRow(db, "SELECT learner_reasoning,self_confidence FROM problem_draft_reflections WHERE draft_id=?", previous["id"])
			oldReasoning := ""
			var oldConfidence any
			if prior != nil {
				oldReasoning = strVal(prior["learner_reasoning"])
				oldConfidence = prior["self_confidence"]
			}
			sameConfidence := selfConfidence == nil && oldConfidence == nil
			if selfConfidence != nil && oldConfidence != nil {
				sameConfidence = oldConfidence.(float64) == *selfConfidence
			}
			if oldReasoning == reasoning && sameConfidence {
				draftID = strVal(previous["id"])
				duplicate = true
			}
		}
		if !duplicate {
			draftID = uuid()
			exec(db, "INSERT INTO problem_drafts VALUES(?,?,?,?)", draftID, id, draft, now())
			if reasoning != "" || selfConfidence != nil {
				var confidence any
				if selfConfidence != nil {
					confidence = *selfConfidence
				}
				exec(db, "INSERT INTO problem_draft_reflections(draft_id,learner_reasoning,self_confidence,recorded_at) VALUES(?,?,?,?)", draftID, reasoning, confidence, now())
			}
		}
		return Value{"draft_id": draftID, "duplicate": duplicate, "problem": problemPublic(db, id, true)}
	})
}
func (s *Store) RecordProblemHint(id string, level int, guidance string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(level >= 0 && level <= 5, "hint level must be an integer 0..5")
		guidance = mustString(guidance, "guidance")
		p := problemPublic(db, id, false)
		insist(strVal(p["status"]) == "open" && strVal(p["mode"]) != "exam", "hints require an open non-exam problem")
		last := getRow(db, "SELECT MAX(level) AS last FROM problem_hints WHERE problem_id=?", id)
		if last["last"] != nil {
			insist(level >= intVal(last["last"]), "hint levels cannot decrease")
		}
		hintID := uuid()
		exec(db, "INSERT INTO problem_hints VALUES(?,?,?,?,?)", hintID, id, level, guidance, now())
		return Value{"hint_id": hintID, "problem": problemPublic(db, id, true)}
	})
}
func (s *Store) ReviewProblemDraft(id, verdict, feedback string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(verdict, "valid", "gap", "incorrect"), "invalid review verdict")
		feedback = mustString(feedback, "feedback")
		p := problemPublic(db, id, false)
		insist(strVal(p["status"]) == "open" && strVal(p["mode"]) != "exam", "draft feedback requires an open non-exam problem")
		draft := getRow(db, "SELECT id FROM problem_drafts WHERE problem_id=? ORDER BY rowid DESC LIMIT 1", id)
		insist(draft != nil, "save a learner draft before reviewing")
		reviewID := uuid()
		exec(db, "INSERT INTO problem_reviews VALUES(?,?,?,?,?,?)", reviewID, id, strVal(draft["id"]), verdict, feedback, now())
		return Value{"review_id": reviewID, "problem": problemPublic(db, id, true)}
	})
}
func (s *Store) FinishProblem(id, outcome, evaluation string, confidence float64, credits []string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(outcome, "solved", "incomplete", "abandoned"), "invalid problem outcome")
		insist(confidence >= 0 && confidence <= 1, "confidence must be a number between 0 and 1")
		evaluation = mustString(evaluation, "evaluation")
		insist(stringsUnique(credits), "credited_concept_ids must contain distinct identifiers")
		p := problemPublic(db, id, false)
		insist(strVal(p["status"]) == "open", "problem is already closed")
		drafts := countOrPanic(db, "SELECT COUNT(*) FROM problem_drafts WHERE problem_id=?", id)
		insist(outcome == "abandoned" || drafts > 0, "save the learner's attempt before completing")
		hints := countOrPanic(db, "SELECT COUNT(*) FROM problem_hints WHERE problem_id=?", id)
		reviews := countOrPanic(db, "SELECT COUNT(*) FROM problem_reviews WHERE problem_id=?", id)
		independent := outcome == "solved" && confidence >= minConfidence && valid(strVal(p["mode"]), "independent", "exam") && hints == 0 && reviews == 0
		insist(len(credits) == 0 || independent, "concept credit requires an independently solved problem")
		assigned := set(p["concept_ids"].([]string))
		for _, cid := range credits {
			insist(assigned[cid], "credited concepts must be attached to the problem")
		}
		exec(db, "UPDATE problems SET status=?,final_evaluation=?,final_confidence=?,independently_solved=?,completed_at=? WHERE id=?", outcome, evaluation, confidence, independent, now(), id)
		for _, cid := range credits {
			exec(db, "UPDATE problem_concepts SET credited=1 WHERE problem_id=? AND concept_id=?", id, cid)
		}
		return problemPublic(db, id, true)
	})
}
func (s *Store) GetProblemSolution(id string) (Value, error) {
	return s.with(func(db *database) Value {
		r := ensureRow(getRow(db, "SELECT id,status,reference_solution,grading_rubric FROM problems WHERE id=?", id), "problem", id)
		insist(strVal(r["status"]) != "open", "reference solutions remain sealed until the problem is closed")
		return Value(r)
	})
}
