package store

import (
	"sort"
	"time"
)

var practiceMethods = []string{"recall", "discrimination", "composition", "solve"}
var missClasses = []string{"none", "cue", "discrimination", "execution", "careless", "structural"}

func practicePublic(db *database, id string) Value {
	ses := ensureRow(getRow(db, "SELECT * FROM practice_sessions WHERE id=?", id), "practice session", id)
	targets := []string{}
	for _, r := range getRows(db, "SELECT concept_id FROM practice_targets WHERE session_id=? ORDER BY concept_id", id) {
		targets = append(targets, strVal(r["concept_id"]))
	}
	problems := []Value{}
	for _, r := range getRows(db, `SELECT i.id,i.problem_id,i.target_concept_id,i.method,i.cue_class,i.difficulty,p.status FROM practice_items i JOIN problems p ON p.id=i.problem_id WHERE i.session_id=? ORDER BY i.created_at,i.rowid`, id) {
		r["outcome"] = getRow(db, "SELECT correct,miss_class,highest_hint,remarks,recorded_at FROM practice_outcomes WHERE practice_item_id=?", r["id"])
		problems = append(problems, Value(r))
	}
	return Value{"id": id, "goal": ses["goal"], "state": ses["state"], "targets": targets, "items": problems, "created_at": ses["created_at"]}
}
func (s *Store) StartPracticeSession(goal string, conceptIDs []string) (Value, error) {
	return s.with(func(db *database) Value {
		goal = mustString(goal, "goal")
		insist(len(conceptIDs) > 0 && len(conceptIDs) <= 100 && stringsUnique(conceptIDs), "select 1..100 distinct practice concepts")
		id := uuid()
		for _, cid := range conceptIDs {
			ensureRow(getRow(db, "SELECT id FROM concepts WHERE id=?", cid), "concept", cid)
		}
		exec(db, "INSERT INTO practice_sessions(id,goal,state,created_at) VALUES(?,?,'active',?)", id, goal, now())
		for _, cid := range conceptIDs {
			exec(db, "INSERT INTO practice_targets VALUES(?,?)", id, cid)
		}
		return practicePublic(db, id)
	})
}
func (s *Store) GetPracticeSession(id string) (Value, error) {
	return s.with(func(db *database) Value { return practicePublic(db, id) })
}
func (s *Store) ListPracticeSessions(limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 100, "limit must be 1..100")
		out := []Value{}
		for _, r := range getRows(db, "SELECT id FROM practice_sessions ORDER BY created_at DESC LIMIT ?", limit) {
			out = append(out, practicePublic(db, strVal(r["id"])))
		}
		return Value{"sessions": out}
	})
}

// PracticePlan returns a demand class, not an invented problem. The host must
// create a genuine novel situation and commit its solution before presenting it.
func practicePlan(db *database, sessionID string) Value {
	session := ensureRow(getRow(db, "SELECT id,state FROM practice_sessions WHERE id=?", sessionID), "practice session", sessionID)
	insist(strVal(session["state"]) == "active", "practice session is closed")
	active := getRow(db, `SELECT i.problem_id FROM practice_items i JOIN problems p ON p.id=i.problem_id WHERE i.session_id=? AND p.status='open' ORDER BY i.created_at LIMIT 1`, sessionID)
	if active != nil {
		return Value{"action": "finish_active_problem", "problem_id": active["problem_id"]}
	}
	targets := getRows(db, "SELECT concept_id FROM practice_targets WHERE session_id=? ORDER BY concept_id", sessionID)
	insist(len(targets) > 0, "practice session has no targets")
	last := getRow(db, "SELECT target_concept_id FROM practice_items WHERE session_id=? ORDER BY rowid DESC LIMIT 1", sessionID)
	lastID := strVal(last["target_concept_id"])
	type choice struct {
		id, method        string
		score, difficulty int
		recent            string
		retest            bool
	}
	options := []choice{}
	for _, targetRow := range targets {
		cid := strVal(targetRow["concept_id"])
		counts := getRows(db, `SELECT i.method,COUNT(*) AS n FROM practice_items i JOIN practice_outcomes o ON o.practice_item_id=i.id WHERE i.session_id=? AND i.target_concept_id=? AND o.correct=1 AND o.highest_hint<2 GROUP BY i.method`, sessionID, cid)
		successes := map[string]int{}
		for _, r := range counts {
			successes[strVal(r["method"])] = intVal(r["n"])
		}
		method := "recall"
		if successes["recall"] >= 2 {
			method = "discrimination"
		}
		if successes["discrimination"] >= 2 {
			method = "composition"
		}
		if successes["composition"] >= 2 {
			method = "solve"
		}
		done := countOrPanic(db, "SELECT COUNT(*) FROM practice_items WHERE session_id=? AND target_concept_id=?", sessionID, cid)
		total := successes["recall"] + successes["discrimination"] + successes["composition"] + successes["solve"]
		prior := getRow(db, `SELECT i.created_at,o.correct,i.difficulty FROM practice_items i LEFT JOIN practice_outcomes o ON o.practice_item_id=i.id WHERE i.session_id=? AND i.target_concept_id=? ORDER BY i.rowid DESC LIMIT 1`, sessionID, cid)
		difficulty := 2
		retest := false
		if prior != nil {
			difficulty = intVal(prior["difficulty"])
			if prior["correct"] != nil {
				if boolVal(prior["correct"]) {
					if difficulty < 5 {
						difficulty++
					}
				} else {
					if difficulty > 1 {
						difficulty--
					}
					retest = true
				}
			}
		}
		// Prefer targets with less demonstrated successful retrieval, then
		// targets not just practised. A demonstrated gap gets priority.
		score := total*10 + done
		if cid == lastID && len(targets) > 1 {
			score += 100
		}
		if retest {
			score -= 5
		}
		options = append(options, choice{id: cid, method: method, score: score, difficulty: difficulty, retest: retest})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].score != options[j].score {
			return options[i].score < options[j].score
		}
		return options[i].id < options[j].id
	})
	sel := options[0]
	cues := []string{}
	for _, r := range getRows(db, "SELECT cue_class FROM practice_items WHERE session_id=? AND target_concept_id=? ORDER BY rowid DESC LIMIT 20", sessionID, sel.id) {
		cues = append(cues, strVal(r["cue_class"]))
	}
	return Value{"action": "pose", "session_id": sessionID, "target_concept_id": sel.id, "method": sel.method, "difficulty": sel.difficulty, "avoid_cue_classes": cues, "fresh_surface_required": true, "retest_obligation": sel.retest, "solution_must_be_committed": true}
}
func (s *Store) RecommendPractice(sessionID string) (Value, error) {
	return s.with(func(db *database) Value { return practicePlan(db, sessionID) })
}
func (s *Store) CreatePracticeProblem(sessionID, targetID, method, cue string, difficulty int, title, statement, kind, discipline, solution, rubric string) (Value, error) {
	return s.with(func(db *database) Value {
		session := ensureRow(getRow(db, "SELECT state FROM practice_sessions WHERE id=?", sessionID), "practice session", sessionID)
		insist(strVal(session["state"]) == "active", "practice session is closed")
		ensureRow(getRow(db, "SELECT 1 FROM practice_targets WHERE session_id=? AND concept_id=?", sessionID, targetID), "practice target", targetID)
		insist(valid(method, practiceMethods...), "invalid practice method")
		insist(difficulty >= 1 && difficulty <= 5, "difficulty must be 1..5")
		cue = mustString(cue, "cue_class")
		insist(len(cue) <= 150, "cue class is too long")
		insist(getRow(db, `SELECT 1 FROM practice_items i JOIN problems p ON p.id=i.problem_id WHERE i.session_id=? AND p.status='open'`, sessionID) == nil, "complete the active problem before posing another")
		insist(getRow(db, "SELECT 1 FROM practice_items WHERE session_id=? AND target_concept_id=? AND method=? AND cue_class=?", sessionID, targetID, method, cue) == nil, "cue class already used for this target and practice method")
		insist(getRow(db, `SELECT 1 FROM practice_items i JOIN problems p ON p.id=i.problem_id WHERE i.session_id=? AND lower(trim(p.statement))=lower(trim(?))`, sessionID, statement) == nil, "identical problem surface cannot be repeated in a practice session")
		// The reference solution and rubric are sealed by the regular problem store.
		problemDifficulty := "standard"
		if difficulty == 1 {
			problemDifficulty = "foundational"
		} else if difficulty >= 3 && difficulty < 5 {
			problemDifficulty = "challenging"
		} else if difficulty == 5 {
			problemDifficulty = "research"
		}
		exercise := createProblem(db, title, statement, kind, "independent", problemDifficulty, []string{targetID}, discipline, "generated", solution, rubric, "")
		id := uuid()
		exec(db, "INSERT INTO practice_items VALUES(?,?,?,?,?,?,?,?)", id, sessionID, exercise["id"], targetID, method, cue, difficulty, now())
		return Value{"practice_item_id": id, "exercise": exercise, "session": practicePublic(db, sessionID)}
	})
}
func (s *Store) RecordPracticeOutcome(itemID string, correct bool, missClass, remarks string) (Value, error) {
	return s.with(func(db *database) Value {
		item := ensureRow(getRow(db, "SELECT * FROM practice_items WHERE id=?", itemID), "practice item", itemID)
		p := ensureRow(getRow(db, "SELECT id,status FROM problems WHERE id=?", item["problem_id"]), "problem", strVal(item["problem_id"]))
		insist(strVal(p["status"]) != "open", "finish the problem before recording practice outcomes")
		insist(!correct || strVal(p["status"]) == "solved", "a correct outcome requires a solved problem")
		insist(valid(missClass, missClasses...), "invalid practice miss classification")
		insist((correct && missClass == "none") || (!correct && missClass != "none"), "miss classification must match correctness")
		remarks = mustString(remarks, "remarks")
		prior := getRow(db, "SELECT 1 FROM practice_outcomes WHERE practice_item_id=?", itemID)
		insist(prior == nil, "practice outcome already recorded")
		maxHint := getRow(db, "SELECT MAX(level) AS highest FROM problem_hints WHERE problem_id=?", item["problem_id"])
		hint := -1
		if maxHint["highest"] != nil {
			hint = intVal(maxHint["highest"])
		}
		exec(db, "INSERT INTO practice_outcomes VALUES(?,?,?,?,?,?)", itemID, correct, missClass, hint, remarks, now())
		cid := strVal(item["target_concept_id"])
		old := getRow(db, "SELECT successes,interval_days FROM review_schedule WHERE concept_id=?", cid)
		successes, interval := 0, 1
		if correct && hint <= 1 {
			if old != nil {
				successes = intVal(old["successes"])
			}
			successes++
			intervals := []int{1, 3, 7, 14, 30, 60, 120}
			idx := successes - 1
			if idx >= len(intervals) {
				idx = len(intervals) - 1
			}
			interval = intervals[idx]
		}
		due := time.Now().UTC().Add(time.Duration(interval) * 24 * time.Hour).Format("2006-01-02T15:04:05.000000+00:00")
		exec(db, `INSERT INTO review_schedule(concept_id,due_at,interval_days,successes,last_evaluated_at) VALUES(?,?,?,?,?)
        ON CONFLICT(concept_id) DO UPDATE SET due_at=excluded.due_at,interval_days=excluded.interval_days,successes=excluded.successes,last_evaluated_at=excluded.last_evaluated_at`, cid, due, interval, successes, now())
		return Value{"practice_item_id": itemID, "correct": correct, "miss_class": missClass, "highest_hint": hint, "review_due_at": due, "next": practicePlan(db, strVal(item["session_id"]))}
	})
}
func (s *Store) GetReviewQueue(limit int, includeUpcoming bool) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 100, "limit must be 1..100")
		q := "SELECT concept_id,due_at,interval_days,successes FROM review_schedule WHERE due_at<=? ORDER BY due_at LIMIT ?"
		args := []any{now(), limit}
		if includeUpcoming {
			q = "SELECT concept_id,due_at,interval_days,successes FROM review_schedule ORDER BY due_at LIMIT ?"
			args = []any{limit}
		}
		return Value{"reviews": mapRows(getRows(db, q, args...))}
	})
}
func (s *Store) ClosePracticeSession(sessionID string) (Value, error) {
	return s.with(func(db *database) Value {
		r := ensureRow(getRow(db, "SELECT state FROM practice_sessions WHERE id=?", sessionID), "practice session", sessionID)
		insist(strVal(r["state"]) == "active", "practice session already closed")
		insist(getRow(db, `SELECT 1 FROM practice_items i JOIN problems p ON p.id=i.problem_id WHERE i.session_id=? AND p.status='open'`, sessionID) == nil, "cannot close with an active problem")
		exec(db, "UPDATE practice_sessions SET state='closed',closed_at=? WHERE id=?", now(), sessionID)
		return practicePublic(db, sessionID)
	})
}
