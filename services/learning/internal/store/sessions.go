package store

import (
	"errors"
	"fmt"
)

var sessionTransitions = map[string][]string{
	"diagnose": {"plan"}, "plan": {"diagnose", "teach"}, "teach": {"plan", "practice"},
	"practice": {"teach", "reassess"}, "reassess": {"practice", "complete", "diagnose"}, "complete": {},
}

func sessionRow(db *database, id string) row {
	return ensureRow(getRow(db, "SELECT * FROM learning_sessions WHERE id=?", id), "learning session", id)
}
func checkRevision(r row, expected *int) {
	if expected != nil && *expected != intVal(r["revision"]) {
		panic(errors.New("session revision conflict; reload the session"))
	}
}
func bumpSession(db *database, id string) {
	exec(db, "UPDATE learning_sessions SET revision=revision+1,updated_at=? WHERE id=?", now(), id)
}
func (s *Store) StartLearningSession(goal, conceptID, mode string, targetLevel *int) (Value, error) {
	var id string
	_, err := s.with(func(db *database) Value {
		insist(valid(mode, "learn", "practise", "solve", "assess"), "invalid learning mode")
		goal = mustString(goal, "goal")
		if targetLevel != nil {
			insist(*targetLevel >= 1 && *targetLevel <= 5, "target level must be from 1 to 5")
		}
		ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", conceptID), "concept", conceptID)
		level := target(db)
		if targetLevel != nil {
			level = *targetLevel
		}
		phase := map[string]string{"learn": "diagnose", "practise": "practice", "solve": "practice", "assess": "reassess"}[mode]
		id = uuid()
		now := now()
		exec(db, "INSERT INTO learning_sessions VALUES(?,?,?,?,?,?,0,0,?,?)", id, goal, conceptID, mode, phase, level, now, now)
		return Value{}
	})
	if err != nil {
		return nil, err
	}
	return s.GetLearningSession(id)
}
func foundationSecure(db *database, id string, floor int, visited map[string]bool) bool {
	if visited[id] || levelOnly(db, id) < floor {
		return false
	}
	visited[id] = true
	defer delete(visited, id)
	for _, r := range getRows(db, "SELECT target FROM relations WHERE source=? AND kind='requires'", id) {
		if !foundationSecure(db, strVal(r["target"]), floor, visited) {
			return false
		}
	}
	return true
}
func latestProbeOutcomes(db *database, id string) map[string]row {
	latest := map[string]row{}
	for _, r := range getRows(db, "SELECT * FROM session_probes WHERE session_id=? ORDER BY recorded_at DESC,rowid DESC", id) {
		cid := strVal(r["concept_id"])
		if _, ok := latest[cid]; !ok {
			latest[cid] = r
		}
	}
	return latest
}
func acceptedProbe(r row) bool {
	return r != nil && strVal(r["outcome"]) == "supported" && r["confidence"].(float64) >= minConfidence && !boolVal(r["assisted"])
}
func sessionPublic(db *database, id string, history bool) Value {
	session := Value(sessionRow(db, id))
	version := intVal(session["plan_version"])
	ids := []string{}
	byID := map[string]row{}
	if version > 0 {
		for _, r := range getRows(db, "SELECT * FROM session_steps WHERE session_id=? AND active_version=?", id, version) {
			byID[strVal(r["id"])] = r
		}
		pv := getRow(db, "SELECT step_ids_json FROM session_plan_versions WHERE session_id=? AND version=?", id, version)
		if pv != nil {
			ids = parseList(pv["step_ids_json"])
		}
	}
	relevant := map[string]bool{strVal(session["target_concept_id"]): true}
	for _, stepID := range ids {
		step := byID[stepID]
		if step == nil {
			continue
		}
		relevant[strVal(step["concept_id"])] = true
		for _, p := range parseList(step["premises_json"]) {
			relevant[p] = true
		}
	}
	probes := latestProbeOutcomes(db, id)
	contradicted := map[string]bool{}
	for cid, p := range probes {
		if !acceptedProbe(p) {
			contradicted[cid] = true
		}
	}
	accepted := map[string]bool{}
	for cid := range relevant {
		if !contradicted[cid] && foundationSecure(db, cid, minInt(3, intVal(session["target_level"])), map[string]bool{}) {
			accepted[cid] = true
		}
	}
	supportedInferences := map[string]bool{}
	steps := []Value{}
	for _, stepID := range ids {
		r := byID[stepID]
		if r == nil {
			continue
		}
		step := Value{}
		for k, v := range r {
			if k != "premises_json" {
				step[k] = v
			}
		}
		premises := parseList(r["premises_json"])
		step["premises"] = premises
		latest := getRow(db, "SELECT * FROM session_step_evidence WHERE step_id=? ORDER BY recorded_at DESC,rowid DESC LIMIT 1", stepID)
		if latest == nil {
			step["latest_evaluation"] = nil
		} else {
			step["latest_evaluation"] = Value(latest)
		}
		demonstrated := latest != nil && strVal(latest["verdict"]) == "supported" && latest["confidence"].(float64) >= minConfidence && !boolVal(latest["assisted"])
		missing := []string{}
		for _, p := range premises {
			if !accepted[p] {
				missing = append(missing, p)
			}
		}
		step["premise_gaps"] = missing
		cid := strVal(step["concept_id"])
		if strVal(step["kind"]) == "inference" {
			justification := getRow(db, "SELECT kind FROM justifications WHERE id=?", step["justification_id"])
			kind := strVal(justification["kind"])
			step["justification_kind"] = kind
			supported := demonstrated && len(missing) == 0 && kind != "heuristic"
			step["independently_supported"] = supported
			if supported {
				supportedInferences[cid] = true
			}
		} else {
			num := countOrPanic(db, "SELECT COUNT(*) FROM justifications WHERE concept_id=?", cid)
			step["missing_justification"] = num == 0
			supported := demonstrated && len(missing) == 0 && supportedInferences[cid] && num > 0
			step["independently_supported"] = supported
			if supported {
				accepted[cid] = true
			}
		}
		steps = append(steps, step)
	}
	session["steps"] = steps
	session["secure_foundations"] = sortedSet(accepted)
	missingConcepts := []Value{}
	for _, step := range steps {
		if strVal(step["kind"]) == "concept" && !step["independently_supported"].(bool) {
			missingConcepts = append(missingConcepts, step)
		}
	}
	missingReasons := 0
	for _, step := range missingConcepts {
		hasReason := false
		for _, other := range steps {
			if strVal(other["kind"]) == "inference" && other["concept_id"] == step["concept_id"] && other["independently_supported"].(bool) {
				hasReason = true
				break
			}
		}
		if !hasReason {
			missingReasons++
		}
	}
	session["remaining_steps"] = len(missingConcepts) + missingReasons
	session["plan_ready"] = len(steps) > 0 && len(missingConcepts) == 0
	pending := getRow(db, "SELECT id FROM learning_questions WHERE session_id=? AND status!='graded' ORDER BY rowid DESC LIMIT 1", id)
	if pending == nil {
		session["pending_question"] = nil
	} else {
		session["pending_question"] = questionPublic(db, strVal(pending["id"]))
	}
	if history {
		session["probes"] = mapRows(getRows(db, "SELECT * FROM session_probes WHERE session_id=? ORDER BY recorded_at,rowid", id))
		session["plan_history"] = mapRows(getRows(db, "SELECT version,recorded_at FROM session_plan_versions WHERE session_id=? ORDER BY version", id))
	}
	return session
}
func (s *Store) GetLearningSession(id string) (Value, error) {
	return s.with(func(db *database) Value { return sessionPublic(db, id, true) })
}
func (s *Store) ListLearningSessions(includeCompleted bool, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 100, "invalid session listing options")
		query := "SELECT id FROM learning_sessions WHERE phase!='complete' ORDER BY updated_at DESC,id DESC LIMIT ?"
		if includeCompleted {
			query = "SELECT id FROM learning_sessions ORDER BY updated_at DESC,id DESC LIMIT ?"
		}
		list := []Value{}
		for _, r := range getRows(db, query, limit) {
			list = append(list, sessionPublic(db, strVal(r["id"]), false))
		}
		return Value{"sessions": list}
	})
}
func (s *Store) AdvanceLearningSession(id, next string, revision *int) (Value, error) {
	_, err := s.with(func(db *database) Value {
		row := sessionRow(db, id)
		checkRevision(row, revision)
		insist(getRow(db, "SELECT 1 FROM learning_questions WHERE session_id=? AND status!='graded' LIMIT 1", id) == nil, "assess the pending question before advancing the session")
		insist(valid(next, sessionTransitions[strVal(row["phase"])]...), fmt.Sprintf("cannot advance from %s to %s", row["phase"], next))
		insist(next != "teach" || intVal(row["plan_version"]) > 0, "build a learning plan before teaching")
		if next == "complete" {
			insist(sessionTargetSatisfied(db, row), "session target lacks independently demonstrated prerequisites, suitable problems, or reassessment evidence")
		}
		exec(db, "UPDATE learning_sessions SET phase=? WHERE id=?", next, id)
		bumpSession(db, id)
		return Value{}
	})
	if err != nil {
		return nil, err
	}
	return s.GetLearningSession(id)
}
func (s *Store) RecommendSessionProbe(id string) (Value, error) {
	return s.with(func(db *database) Value {
		session := sessionRow(db, id)
		insist(valid(strVal(session["phase"]), "diagnose", "reassess"), "probes are available during diagnosis or reassessment")
		latest := latestProbeOutcomes(db, id)
		floor := minInt(3, intVal(session["target_level"]))
		needs := func(cid string) bool {
			if r, has := latest[cid]; has {
				return !acceptedProbe(r)
			}
			return !foundationSecure(db, cid, floor, map[string]bool{})
		}
		var descend func(string, map[string]bool) (string, string)
		descend = func(cid string, seen map[string]bool) (string, string) {
			insist(!seen[cid] && len(seen) <= 120, "invalid or overly large diagnostic graph")
			seen[cid] = true
			if !needs(cid) {
				return "", ""
			}
			if latest[cid] == nil {
				return cid, "probe"
			}
			for _, r := range getRows(db, "SELECT target FROM relations WHERE source=? AND kind='requires' ORDER BY target", cid) {
				copySet := map[string]bool{}
				for k, v := range seen {
					copySet[k] = v
				}
				next, action := descend(strVal(r["target"]), copySet)
				if next != "" {
					return next, action
				}
			}
			return cid, "teach"
		}
		cid, action := descend(strVal(session["target_concept_id"]), map[string]bool{})
		if cid == "" {
			return Value{"action": "plan", "concept_id": session["target_concept_id"], "reason": "No further diagnostic gap was found; plan practice or reassessment."}
		}
		c := ensureRow(getRow(db, "SELECT title,summary FROM concepts WHERE id=?", cid), "concept", cid)
		reason := "Repair an observed gap; repeated probing is not a substitute for teaching."
		if action == "probe" {
			reason = "Probe the nearest unresolved idea without revealing the method."
		}
		return Value{"action": action, "concept_id": cid, "title": c["title"], "summary": c["summary"], "reason": reason}
	})
}
func (s *Store) RecordSessionProbe(sessionID, conceptID, question, response, evaluation, outcome string, confidence float64, assisted bool) (Value, error) {
	var probeID string
	_, err := s.with(func(db *database) Value {
		insist(valid(outcome, "supported", "gap", "uncertain"), "invalid probe outcome")
		insist(confidence >= 0 && confidence <= 1, "invalid probe confidence or assistance")
		question = mustString(question, "question")
		response = mustString(response, "learner response")
		evaluation = mustString(evaluation, "evaluation")
		session := sessionRow(db, sessionID)
		insist(valid(strVal(session["phase"]), "diagnose", "reassess"), "cannot probe outside diagnosis or reassessment")
		ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", conceptID), "concept", conceptID)
		probeID = uuid()
		exec(db, "INSERT INTO session_probes VALUES (?,?,?,?,?,?,?,?,?,?)", probeID, sessionID, conceptID, question, response, evaluation, outcome, confidence, assisted, now())
		bumpSession(db, sessionID)
		return Value{}
	})
	if err != nil {
		return nil, err
	}
	result, e := s.GetLearningSession(sessionID)
	if e != nil {
		return nil, e
	}
	return Value{"probe_id": probeID, "session": result}, nil
}

type stepProposal struct {
	key, kind, conceptID, title, justificationID string
	premises                                     []string
}

func (s *Store) BuildLearningPlan(id string, revision *int, preferences map[string]string) (Value, error) {
	var output Value
	_, err := s.with(func(db *database) Value {
		if preferences == nil {
			preferences = map[string]string{}
		}
		session := sessionRow(db, id)
		checkRevision(session, revision)
		insist(valid(strVal(session["phase"]), "plan", "teach", "practice", "reassess"), "advance to planning before building a plan")
		floor := minInt(3, intVal(session["target_level"]))
		latest := latestProbeOutcomes(db, id)
		mastered := func(cid string) bool {
			if p := latest[cid]; p != nil && !acceptedProbe(p) {
				return false
			}
			return foundationSecure(db, cid, floor, map[string]bool{})
		}
		proposals := []stepProposal{}
		frontier := map[string]bool{}
		visiting := map[string]bool{}
		finished := map[string]bool{}
		var walk func(string, bool)
		walk = func(cid string, root bool) {
			if finished[cid] {
				return
			}
			insist(!visiting[cid], "circular reasoning in prerequisite explanations")
			insist(len(finished)+len(visiting) < 120, "learning plan is too large; select a narrower goal")
			if mastered(cid) && !root {
				frontier[cid] = true
				finished[cid] = true
				return
			}
			visiting[cid] = true
			required := map[string]bool{}
			for _, r := range getRows(db, "SELECT target FROM relations WHERE source=? AND kind='requires'", cid) {
				required[strVal(r["target"])] = true
			}
			justifications := getRows(db, "SELECT id,kind,claim,premises_json FROM justifications WHERE concept_id=? ORDER BY created_at,id", cid)
			var selected row
			if preferred, has := preferences[cid]; has {
				for _, j := range justifications {
					if strVal(j["id"]) == preferred {
						selected = j
						break
					}
				}
				insist(selected != nil, "preferred justification does not belong to this concept")
			} else {
				minHeuristic, minUnknown := 2, 1<<30
				for _, j := range justifications {
					heuristic := 0
					if strVal(j["kind"]) == "heuristic" {
						heuristic = 1
					}
					unknown := 0
					for _, p := range parseList(j["premises_json"]) {
						if !mastered(p) {
							unknown++
						}
					}
					if heuristic < minHeuristic || (heuristic == minHeuristic && unknown < minUnknown) {
						selected = j
						minHeuristic = heuristic
						minUnknown = unknown
					}
				}
			}
			premises := []string{}
			if selected != nil {
				premises = parseList(selected["premises_json"])
			}
			for _, p := range premises {
				required[p] = true
			}
			for _, p := range sortedSet(required) {
				walk(p, false)
			}
			if selected != nil {
				jid := strVal(selected["id"])
				proposals = append(proposals, stepProposal{"inference:" + jid, "inference", cid, strVal(selected["claim"]), jid, premises})
			}
			concept := ensureRow(getRow(db, "SELECT title FROM concepts WHERE id=?", cid), "concept", cid)
			proposals = append(proposals, stepProposal{"concept:" + cid, "concept", cid, strVal(concept["title"]), "", sortedSet(required)})
			delete(visiting, cid)
			finished[cid] = true
		}
		walk(strVal(session["target_concept_id"]), true)
		version := intVal(session["plan_version"]) + 1
		ids := []string{}
		for _, p := range proposals {
			previous := getRow(db, "SELECT id FROM session_steps WHERE session_id=? AND step_key=?", id, p.key)
			var stepID string
			if previous != nil {
				stepID = strVal(previous["id"])
				exec(db, "UPDATE session_steps SET title=?,premises_json=?,active_version=? WHERE id=?", p.title, js(p.premises), version, stepID)
			} else {
				stepID = uuid()
				var jid any
				if p.justificationID != "" {
					jid = p.justificationID
				}
				exec(db, "INSERT INTO session_steps VALUES(?,?,?,?,?,?,?,?,?,?)", stepID, id, p.key, p.kind, p.conceptID, jid, p.title, js(p.premises), version, now())
			}
			ids = append(ids, stepID)
		}
		exec(db, "INSERT INTO session_plan_versions VALUES(?,?,?,?)", id, version, js(ids), now())
		exec(db, "UPDATE learning_sessions SET plan_version=? WHERE id=?", version, id)
		bumpSession(db, id)
		output = Value{"plan_version": version, "verified_frontier": sortedSet(frontier)}
		return Value{}
	})
	if err != nil {
		return nil, err
	}
	session, e := s.GetLearningSession(id)
	if e != nil {
		return nil, e
	}
	output["session"] = session
	return output, nil
}
func (s *Store) RecordLearningStep(sessionID, stepID, response, evaluation, verdict string, confidence float64, assisted bool) (Value, error) {
	var evidenceID string
	_, err := s.with(func(db *database) Value {
		insist(valid(verdict, "supported", "gap", "incorrect", "uncertain"), "invalid reasoning verdict")
		insist(confidence >= 0 && confidence <= 1, "invalid reasoning confidence or assistance")
		response = mustString(response, "learner response")
		evaluation = mustString(evaluation, "evaluation")
		session := sessionRow(db, sessionID)
		insist(valid(strVal(session["phase"]), "teach", "practice", "reassess"), "step assessment requires an active teaching or practice phase")
		step := getRow(db, "SELECT 1 FROM session_steps WHERE id=? AND session_id=? AND active_version=?", stepID, sessionID, intVal(session["plan_version"]))
		insist(step != nil, "unknown or inactive learning step")
		evidenceID = uuid()
		exec(db, "INSERT INTO session_step_evidence VALUES(?,?,?,?,?,?,?,?)", evidenceID, stepID, response, evaluation, verdict, confidence, assisted, now())
		bumpSession(db, sessionID)
		return Value{}
	})
	if err != nil {
		return nil, err
	}
	session, e := s.GetLearningSession(sessionID)
	if e != nil {
		return nil, e
	}
	return Value{"evidence_id": evidenceID, "session": session}, nil
}
