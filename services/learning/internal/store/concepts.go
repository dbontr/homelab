package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) AddConcept(id, title, summary, domain string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(slug.MatchString(id), "identifier must be lowercase kebab-case")
		title = mustString(title, "title")
		summary = mustString(summary, "summary")
		exec(db, "INSERT INTO concepts VALUES(?,?,?,?,?)", id, title, summary, strings.TrimSpace(domain), now())
		return concept(db, id)
	})
}
func concept(db *database, id string) Value {
	r := ensureRow(getRow(db, "SELECT * FROM concepts WHERE id=?", id), "concept", id)
	v := Value(r)
	v["mastery"] = mastery(db, id)
	just := mapRows(getRows(db, "SELECT * FROM justifications WHERE concept_id=? ORDER BY created_at,id", id))
	for _, j := range just {
		j["premises"] = parseList(j["premises_json"])
		delete(j, "premises_json")
	}
	v["justifications"] = just
	v["prerequisites"] = mapRows(getRows(db, "SELECT r.target AS id,c.title,r.rationale FROM relations r JOIN concepts c ON c.id=r.target WHERE r.source=? AND r.kind='requires' ORDER BY c.title", id))
	return v
}
func (s *Store) Concept(id string) (Value, error) {
	return s.with(func(db *database) Value { return concept(db, id) })
}
func (s *Store) ConnectConcepts(source, target, kind, rationale string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(kind, "requires", "explains", "applies_to", "analogous_to", "contrasts_with"), "kind must be one of requires, explains, applies_to, analogous_to, contrasts_with")
		rationale = mustString(rationale, "rationale")
		insist(source != target, "self-links are not allowed")
		for _, id := range []string{source, target} {
			ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", id), "concept", id)
		}
		if kind == "requires" {
			path := getRow(db, `WITH RECURSIVE predecessors(id) AS (
      SELECT ? UNION SELECT r.target FROM relations r JOIN predecessors p ON r.source=p.id WHERE r.kind='requires'
    ) SELECT 1 FROM predecessors WHERE id=? LIMIT 1`, target, source)
			insist(path == nil, "prerequisite graph must be acyclic")
		}
		exec(db, "INSERT INTO relations VALUES(?,?,?,?) ON CONFLICT(source,target,kind) DO UPDATE SET rationale=excluded.rationale", source, target, kind, rationale)
		return Value{"source": source, "target": target, "kind": kind, "rationale": rationale}
	})
}
func (s *Store) AddJustification(conceptID, claim, kind string, premises []string, explanation, sourceRef string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(kind, "deductive", "empirical", "axiomatic", "definitional", "model", "heuristic"), "invalid justification kind")
		insist(kind != "deductive" || len(premises) > 0, "deductive justifications require explicit premises")
		insist(kind != "empirical" || strings.TrimSpace(sourceRef) != "", "empirical justifications require a source reference")
		claim = mustString(claim, "claim")
		explanation = mustString(explanation, "explanation")
		sourceRef = strings.TrimSpace(sourceRef)
		for _, id := range append([]string{conceptID}, premises...) {
			ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", id), "concept", id)
		}
		id := uuid()
		exec(db, "INSERT INTO justifications VALUES(?,?,?,?,?,?,?,?)", id, conceptID, claim, kind, js(premises), explanation, sourceRef, now())
		return Value{"id": id, "concept_id": conceptID, "claim": claim, "kind": kind, "premises": premises, "explanation": explanation, "source_reference": sourceRef}
	})
}
func (s *Store) RecordEvidence(conceptID, dimension, question, response, evaluation string, passed bool, confidence float64, assisted bool) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(dimension, dimensions...), "dimension must be one of "+strings.Join(dimensions, ", "))
		insist(confidence >= 0 && confidence <= 1, "confidence must be between 0 and 1")
		insist(dimension != "independent_reconstruction" || !assisted || !passed, "assisted work cannot demonstrate independent reconstruction")
		question = mustString(question, "question")
		response = mustString(response, "learner_response")
		evaluation = mustString(evaluation, "evaluation")
		ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", conceptID), "concept", conceptID)
		timestamp := now()
		if dimension == "retention" && passed {
			insist(!assisted, "assisted reassessment cannot demonstrate retained independent understanding")
			reconstruction := getRow(db, "SELECT passed,confidence,assisted,recorded_at FROM evidence WHERE concept_id=? AND dimension='independent_reconstruction' ORDER BY recorded_at DESC,rowid DESC LIMIT 1", conceptID)
			insist(reconstruction != nil && boolVal(reconstruction["passed"]) && !boolVal(reconstruction["assisted"]) && reconstruction["confidence"].(float64) >= minConfidence, "retention requires a current qualifying independent reconstruction")
			stamp, e := time.Parse(time.RFC3339Nano, strVal(reconstruction["recorded_at"]))
			insist(e == nil && time.Since(stamp) >= 24*time.Hour, "retention requires an independent reconstruction at least 24 hours earlier")
		}
		id := uuid()
		exec(db, "INSERT INTO evidence VALUES(?,?,?,?,?,?,?,?,?,?)", id, conceptID, dimension, question, response, evaluation, passed, confidence, assisted, timestamp)
		return Value{"evidence_id": id, "mastery": mastery(db, conceptID)}
	})
}

// acceptedEvidence examines the latest evidence for each dimension. Delayed
// retention applies to the latest independent reconstruction, not any older
// success, and cannot be established by assisted or premature retesting.
func acceptedEvidence(rows []row) map[string]bool {
	latest := map[string]row{}
	for _, r := range rows {
		d := strVal(r["dimension"])
		if _, seen := latest[d]; !seen {
			latest[d] = r
		}
	}
	accepted := map[string]bool{}
	for _, d := range dimensions {
		r := latest[d]
		accepted[d] = r != nil && boolVal(r["passed"]) && r["confidence"].(float64) >= minConfidence && (d != "independent_reconstruction" && d != "retention" || !boolVal(r["assisted"]))
	}
	reconstruction, retention := latest["independent_reconstruction"], latest["retention"]
	if !accepted["independent_reconstruction"] || !accepted["retention"] {
		accepted["retention"] = false
	} else {
		start, startErr := time.Parse(time.RFC3339Nano, strVal(reconstruction["recorded_at"]))
		end, endErr := time.Parse(time.RFC3339Nano, strVal(retention["recorded_at"]))
		accepted["retention"] = startErr == nil && endErr == nil && end.Sub(start) >= 24*time.Hour
	}
	return accepted
}

func levelOnly(db *database, id string) int {
	rows := getRows(db, "SELECT dimension,passed,confidence,assisted,recorded_at FROM evidence WHERE concept_id=? ORDER BY recorded_at DESC,rowid DESC", id)
	accepted := acceptedEvidence(rows)
	if !accepted["recognition"] {
		return 0
	}
	if !accepted["application"] {
		return 1
	}
	if !accepted["justification"] {
		return 2
	}
	if !accepted["transfer"] {
		return 3
	}
	if !accepted["independent_reconstruction"] || !accepted["retention"] {
		return 4
	}
	return 5
}
func practiceMetrics(db *database, id string) Value {
	r := getRow(db, `SELECT COUNT(*) AS assigned,
 SUM(CASE WHEN p.status!='open' THEN 1 ELSE 0 END) AS completed,
 SUM(CASE WHEN p.status='solved' THEN 1 ELSE 0 END) AS solved,
 SUM(CASE WHEN p.independently_solved=1 AND pc.credited=1 THEN 1 ELSE 0 END) AS independent_solutions
 FROM problem_concepts pc JOIN problems p ON p.id=pc.problem_id WHERE pc.concept_id=?`, id)
	out := Value{}
	for _, k := range []string{"assigned", "completed", "solved", "independent_solutions"} {
		out[k] = intVal(r[k])
	}
	return out
}
func target(db *database) int {
	var n int
	fmt.Sscan(strVal(getRow(db, "SELECT value FROM settings WHERE name='target_level'")["value"]), &n)
	return n
}
func mastery(db *database, id string) Value {
	rows := getRows(db, "SELECT * FROM evidence WHERE concept_id=? ORDER BY recorded_at DESC,rowid DESC", id)
	accepted := acceptedEvidence(rows)
	missing := []string{}
	for _, d := range dimensions {
		if !accepted[d] {
			missing = append(missing, d)
		}
	}
	level := levelOnly(db, id)
	goal := target(db)
	gaps := []string{}
	for _, r := range getRows(db, "SELECT target FROM relations WHERE source=? AND kind='requires'", id) {
		p := strVal(r["target"])
		if levelOnly(db, p) < minInt(goal, 3) {
			gaps = append(gaps, p)
		}
	}
	pm := practiceMetrics(db, id)
	required := 0
	if goal == 4 {
		required = 1
	} else if goal == 5 {
		required = 2
	}
	rigorous := Value(nil)
	qualified := true
	if goal >= 4 && level >= 4 {
		full := masteryPortfolio(db, id)
		rigorous = Value{
			"distinct_difficult_solutions":   full["distinct_difficult_solutions"],
			"varied_problem_classes":         full["varied_problem_classes"],
			"externally_checked_solutions":   full["externally_checked_solutions"],
			"full_mastery_evidence_recorded": full["full_mastery_evidence_recorded"],
			"unresolved":                     full["unresolved"],
		}
		qualified = intVal(full["distinct_difficult_solutions"]) >= 1
		if goal >= 5 {
			qualified = full["full_mastery_evidence_recorded"].(bool)
		}
	} else if goal >= 4 {
		qualified = false
	}
	return Value{"level": level, "target": goal, "conceptual_meets_target": level >= goal, "meets_target": level >= goal && intVal(pm["independent_solutions"]) >= required && qualified,
		"rigorous_mastery_evidence":      rigorous,
		"required_independent_solutions": required, "practice": pm, "accepted_dimensions": accepted, "missing_dimensions": missing,
		"foundation_gaps": gaps, "assessment_count": len(rows)}
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func (s *Store) SetTarget(level int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(level >= 1 && level <= 5, "target level must be an integer from 1 to 5")
		exec(db, "UPDATE settings SET value=? WHERE name='target_level'", fmt.Sprint(level))
		return Value{"target_level": level, "observed_evidence_unchanged": true}
	})
}
func (s *Store) GetTarget() (Value, error) {
	return s.with(func(db *database) Value { return Value{"target_level": target(db)} })
}
func graph(db *database, focus string, depth, limit int) Value {
	insist(depth >= 1 && depth <= 4 && limit >= 1 && limit <= 200, "depth must be 1..4 and limit must be 1..200")
	ids := []string{}
	if focus != "" {
		ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", focus), "concept", focus)
		visited := map[string]bool{focus: true}
		frontier := map[string]bool{focus: true}
		for i := 0; i < depth && len(frontier) > 0; i++ {
			next := map[string]bool{}
			for id := range frontier {
				for _, r := range getRows(db, "SELECT source,target FROM relations WHERE source=? OR target=?", id, id) {
					next[strVal(r["source"])] = true
					next[strVal(r["target"])] = true
				}
			}
			frontier = map[string]bool{}
			for id := range next {
				if !visited[id] {
					frontier[id] = true
					visited[id] = true
				}
			}
		}
		ids = sortedSet(visited)
		if len(ids) > limit {
			ids = ids[:limit]
		}
	} else {
		for _, r := range getRows(db, "SELECT id FROM concepts ORDER BY title LIMIT ?", limit) {
			ids = append(ids, strVal(r["id"]))
		}
	}
	nodes := []Value{}
	for _, id := range ids {
		v := Value(getRow(db, "SELECT id,title,summary,domain FROM concepts WHERE id=?", id))
		v["mastery"] = mastery(db, id)
		nodes = append(nodes, v)
	}
	if len(ids) == 0 {
		return Value{"nodes": nodes, "edges": []Value{}, "target_level": target(db)}
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	q := "SELECT source,target,kind,rationale FROM relations WHERE source IN (" + placeholders + ") AND target IN (" + placeholders + ") ORDER BY kind,source,target"
	args := make([]any, 0, len(ids)*2)
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	edges := mapRows(getRows(db, q, args...))
	return Value{"nodes": nodes, "edges": edges, "target_level": target(db)}
}
func (s *Store) Graph(focus string, depth, limit int) (Value, error) {
	return s.with(func(db *database) Value { return graph(db, focus, depth, limit) })
}
func (s *Store) Recommend(limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 50, "limit must be 1..50")
		ready := []Value{}
		blocked := []Value{}
		for _, r := range getRows(db, "SELECT id,title,domain FROM concepts ORDER BY title") {
			id := strVal(r["id"])
			m := mastery(db, id)
			if m["meets_target"].(bool) {
				continue
			}
			item := Value{"id": id, "title": r["title"], "domain": r["domain"], "level": m["level"], "missing": m["missing_dimensions"], "foundation_gaps": m["foundation_gaps"]}
			if len(m["foundation_gaps"].([]string)) > 0 {
				blocked = append(blocked, item)
			} else {
				ready = append(ready, item)
			}
		}
		sort.SliceStable(ready, func(i, j int) bool {
			if ready[i]["level"] == ready[j]["level"] {
				return strVal(ready[i]["title"]) < strVal(ready[j]["title"])
			}
			return intVal(ready[i]["level"]) > intVal(ready[j]["level"])
		})
		count := len(blocked)
		if len(blocked) > limit {
			blocked = blocked[:limit]
		}
		if len(ready) > limit {
			ready = ready[:limit]
		}
		return Value{"target_level": target(db), "recommended": ready, "blocked_count": count, "blocked_examples": blocked}
	})
}
func (s *Store) Summary() (Value, error) {
	return s.with(func(db *database) Value {
		ids := getRows(db, "SELECT id FROM concepts")
		levels := make([]int, 0, len(ids))
		verified := 0
		conceptual := 0
		counts := map[string]int{}
		goal := target(db)
		for i := 0; i <= 5; i++ {
			counts[fmt.Sprint(i)] = 0
		}
		for _, r := range ids {
			id := strVal(r["id"])
			level := levelOnly(db, id)
			levels = append(levels, level)
			counts[fmt.Sprint(level)]++
			m := mastery(db, id)
			if m["meets_target"].(bool) {
				verified++
			}
			if level >= goal {
				conceptual++
			}
		}
		return Value{"concepts": len(ids), "target_level": goal, "verified_at_target": verified, "conceptual_at_target": conceptual,
			"open_problems": countOrPanic(db, "SELECT COUNT(*) FROM problems WHERE status='open'"), "solved_problems": countOrPanic(db, "SELECT COUNT(*) FROM problems WHERE status='solved'"), "level_counts": counts}
	})
}
func countOrPanic(db *database, q string, args ...any) int {
	v, e := db.count(q, args...)
	if e != nil {
		panic(e)
	}
	return v
}
