package store

// NextLearningAction identifies the next evidence-producing interaction.
// It does not award mastery or fabricate a learner response. The returned
// revision lets the host fence later state changes against concurrent turns.
func (s *Store) NextLearningAction(id string) (Value, error) {
	session, err := s.GetLearningSession(id)
	if err != nil {
		return nil, err
	}
	phase := strVal(session["phase"])
	goalID := strVal(session["target_concept_id"])
	targetLevel := intVal(session["target_level"])
	out := Value{"session_id": id, "revision": session["revision"], "phase": phase, "mode": session["mode"], "target_concept_id": goalID, "target_level": targetLevel, "action": "", "reason": ""}
	choose := func(action, reason string) {
		out["action"], out["reason"] = action, reason
	}
	if pending := session["pending_question"]; pending != nil {
		question := pending.(Value)
		out["question_id"] = question["id"]
		out["concept_id"] = question["concept_id"]
		out["question_status"] = question["status"]
		if question["status"] == "pending" {
			choose("await_answer", "Preserve the committed question and wait for the learner's response.")
		} else {
			choose("grade_answer", "Evaluate the recorded answer before changing the learning route.")
		}
		return out, nil
	}

	switch phase {
	case "diagnose":
		probe, e := s.RecommendSessionProbe(id)
		if e != nil {
			return nil, e
		}
		out["concept_id"] = probe["concept_id"]
		switch probe["action"] {
		case "probe":
			choose("pose_diagnostic", "Commit one diagnostic question at the nearest uncertain prerequisite.")
		case "teach":
			choose("plan_repair", "An observed gap needs explanation rather than repeated questioning.")
		default:
			out["next_phase"] = "plan"
			choose("advance_phase", "No unresolved diagnostic boundary remains.")
		}
	case "plan":
		if intVal(session["plan_version"]) == 0 {
			choose("build_plan", "Select a justified route from demonstrated prerequisites.")
		} else {
			out["next_phase"] = "teach"
			choose("advance_phase", "A versioned route is available for teaching.")
		}
	case "teach":
		if intVal(session["plan_version"]) == 0 {
			choose("build_plan", "An explicit reasoning route is required before instruction.")
			break
		}
		steps := session["steps"].([]Value)
		if len(steps) == 0 {
			choose("build_plan", "The selected route has no assessable steps.")
			break
		}
		for _, step := range steps {
			if step["independently_supported"] == true {
				continue
			}
			out["step_id"], out["concept_id"] = step["id"], step["concept_id"]
			out["step_kind"] = step["kind"]
			out["step_title"] = step["title"]
			out["premise_gaps"] = step["premise_gaps"]
			if gaps, ok := step["premise_gaps"].([]string); ok && len(gaps) > 0 {
				choose("repair_foundation", "A required premise has not been demonstrated; resolve it first.")
			} else if step["kind"] == "concept" && step["missing_justification"] == true {
				choose("supply_justification", "The route lacks an explicit justification for this concept.")
			} else if latest := step["latest_evaluation"]; latest != nil {
				evaluation := latest.(Value)
				out["last_verdict"] = evaluation["verdict"]
				if evaluation["verdict"] != "supported" || evaluation["assisted"] == int64(1) || evaluation["confidence"].(float64) < minConfidence {
					choose("repair_reasoning", "Revisit the earliest unsupported reasoning step using the learner's actual response.")
				} else {
					choose("reconstruct_step", "Reassess this step independently after earlier assistance or a missing dependency.")
				}
			} else {
				choose("elicit_reasoning", "Ask for an original justification or reconstruction, then assess the actual work.")
			}
			return out, nil
		}
		out["next_phase"] = "practice"
		choose("advance_phase", "Every step in the selected reasoning route has supported independent evidence.")
	case "practice":
		open, e := s.with(func(db *database) Value {
			r := getRow(db, `SELECT p.id,p.title FROM problems p
				JOIN problem_concepts pc ON pc.problem_id=p.id
				WHERE pc.concept_id=? AND p.status='open'
				ORDER BY p.created_at DESC,p.id DESC LIMIT 1`, goalID)
			if r == nil {
				return Value{}
			}
			return Value(r)
		})
		if e != nil {
			return nil, e
		}
		if open["id"] != nil {
			out["problem_id"] = open["id"]
			choose("resume_problem", "Protect the unfinished independent attempt and its saved drafts.")
		} else {
			choose("assign_challenge", "Select a genuinely unfamiliar problem with sufficient difficulty and a sealed rubric.")
		}
	case "reassess":
		ready, e := s.with(func(db *database) Value {
			return Value{"ready": sessionTargetSatisfied(db, sessionRow(db, id))}
		})
		if e != nil {
			return nil, e
		}
		if ready["ready"] == true {
			out["next_phase"] = "complete"
			choose("advance_phase", "The session target has the required recorded evidence; inspect its limitations before making a claim.")
			break
		}
		portfolio, e := s.MasteryPortfolio(goalID)
		if e != nil {
			return nil, e
		}
		out["mastery_obstacles"] = portfolio["unresolved"]
		choose("reassess_independently", "Test missing reconstruction, transfer, or delayed retention without relying on recalled answers.")
	case "complete":
		choose("finished", "The learning session is closed; later reassessment must use a new session.")
	}
	return out, nil
}

// sessionTargetSatisfied is deliberately stricter than a host's assessment
// verdict. Recorded external corroboration is still a provenance claim.
func sessionTargetSatisfied(db *database, session row) bool {
	level := intVal(session["target_level"])
	cid := strVal(session["target_concept_id"])
	if levelOnly(db, cid) < level || !foundationSecure(db, cid, minInt(level, 3), map[string]bool{}) {
		return false
	}
	if level < 4 {
		return true
	}
	portfolio := masteryPortfolio(db, cid)
	if level == 4 {
		return intVal(portfolio["distinct_difficult_solutions"]) >= 1
	}
	return portfolio["full_mastery_evidence_recorded"] == true
}
