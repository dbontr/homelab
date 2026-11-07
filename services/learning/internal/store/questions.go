package store

import (
	"math"
	"strings"
)

type QuestionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func questionPublic(db *database, id string) Value {
	r := ensureRow(getRow(db, "SELECT * FROM learning_questions WHERE id=?", id), "learning question", id)
	out := Value{}
	for _, key := range []string{"id", "session_id", "concept_id", "step_id", "kind", "prompt", "response_kind", "status", "learner_response", "verdict", "evaluation", "confidence", "assisted", "created_at", "answered_at", "graded_at"} {
		out[key] = r[key]
	}
	var options []QuestionOption
	insist(jsonUnmarshal(strVal(r["options_json"]), &options) == nil, "invalid question options")
	out["options"] = options
	out["assisted"] = boolVal(r["assisted"])
	reflection := getRow(db, "SELECT learner_reasoning,self_confidence FROM question_reflections WHERE question_id=?", id)
	out["learner_reasoning"] = ""
	out["self_confidence"] = nil
	if reflection != nil {
		out["learner_reasoning"] = reflection["learner_reasoning"]
		out["self_confidence"] = reflection["self_confidence"]
	}
	out["requires_reasoning_review"] = strVal(r["kind"]) == "diagnostic"
	out["reasoning_verdict"] = nil
	out["reasoning_evaluation"] = nil
	if strVal(r["status"]) == "graded" {
		out["expected_answer"] = r["expected_answer"]
		out["grading_criteria"] = r["grading_criteria"]
		if strVal(r["response_kind"]) == "multiple_choice" {
			out["answer_correct"] = strVal(r["learner_response"]) == strVal(r["expected_answer"])
		}
		review := getRow(db, "SELECT verdict,evaluation FROM question_reasoning_reviews WHERE question_id=?", id)
		if review != nil {
			out["reasoning_verdict"] = review["verdict"]
			out["reasoning_evaluation"] = review["evaluation"]
		}
	}
	return out
}
func (s *Store) OpenLearningQuestion(sessionID, conceptID, stepID, kind, prompt, responseKind string, options []QuestionOption, expectedAnswer, criteria string) (Value, error) {
	return s.with(func(db *database) Value {
		session := ensureRow(getRow(db, "SELECT phase FROM learning_sessions WHERE id=?", sessionID), "learning session", sessionID)
		insist(valid(kind, "diagnostic", "prediction", "check"), "invalid question kind")
		if kind == "diagnostic" {
			insist(valid(strVal(session["phase"]), "diagnose", "reassess"), "diagnostic question requires diagnosis or reassessment")
		}
		if kind != "diagnostic" {
			insist(strVal(session["phase"]) == "teach", "prediction/check requires teaching phase")
		}
		ensureRow(getRow(db, "SELECT 1 FROM concepts WHERE id=?", conceptID), "concept", conceptID)
		if stepID != "" {
			ensureRow(getRow(db, "SELECT 1 FROM session_steps WHERE session_id=? AND id=?", sessionID, stepID), "learning step", stepID)
		}
		insist(getRow(db, "SELECT 1 FROM learning_questions WHERE session_id=? AND status!='graded'", sessionID) == nil, "respond to and assess the pending question before posing another")
		insist(valid(responseKind, "free_response", "multiple_choice"), "invalid response kind")
		if responseKind == "multiple_choice" {
			insist(len(options) >= 2 && len(options) <= 8, "multiple choice needs 2..8 options")
			keys := map[string]bool{}
			for _, o := range options {
				insist(strings.TrimSpace(o.ID) != "" && len(o.ID) <= 32 && !keys[o.ID], "option IDs must be distinct and nonempty")
				insist(strings.TrimSpace(o.Label) != "" && len(o.Label) <= 1000, "option label invalid")
				keys[o.ID] = true
			}
		} else {
			insist(len(options) == 0, "free response must not have multiple-choice options")
		}
		prompt = mustString(prompt, "prompt")
		expectedAnswer = mustString(expectedAnswer, "expected_answer")
		if responseKind == "multiple_choice" {
			matched := false
			for _, option := range options {
				if option.ID == expectedAnswer {
					matched = true
				}
			}
			insist(matched, "expected answer must be one of the option IDs")
		}
		criteria = mustString(criteria, "grading_criteria")
		id := uuid()
		var stepArg any = nil
		if stepID != "" {
			stepArg = stepID
		}
		exec(db, `INSERT INTO learning_questions(id,session_id,concept_id,step_id,kind,prompt,response_kind,options_json,expected_answer,grading_criteria,status,created_at)
            VALUES(?,?,?,?,?,?,?,?,?,?,'pending',?)`, id, sessionID, conceptID, stepArg, kind, prompt, responseKind, js(options), expectedAnswer, criteria, now())
		bumpSession(db, sessionID)
		return questionPublic(db, id)
	})
}
func (s *Store) GetLearningQuestion(id string) (Value, error) {
	return s.with(func(db *database) Value { return questionPublic(db, id) })
}
func (s *Store) PendingLearningQuestion(sessionID string) (Value, error) {
	return s.with(func(db *database) Value {
		ensureRow(getRow(db, "SELECT 1 FROM learning_sessions WHERE id=?", sessionID), "learning session", sessionID)
		r := getRow(db, "SELECT id FROM learning_questions WHERE session_id=? AND status!='graded' ORDER BY rowid DESC LIMIT 1", sessionID)
		if r == nil {
			return Value{"pending": false}
		}
		return Value{"pending": true, "question": questionPublic(db, strVal(r["id"]))}
	})
}
func (s *Store) AnswerLearningQuestion(id, response string, assisted bool) (Value, error) {
	return s.AnswerLearningQuestionWithReflection(id, response, assisted, "", nil)
}

// AnswerLearningQuestionWithReflection saves the original answer, explanation,
// and learner confidence before any grading or answer-key disclosure.
func (s *Store) AnswerLearningQuestionWithReflection(id, response string, assisted bool, reasoning string, selfConfidence *float64) (Value, error) {
	return s.with(func(db *database) Value {
		q := ensureRow(getRow(db, "SELECT * FROM learning_questions WHERE id=?", id), "learning question", id)
		insist(strVal(q["status"]) == "pending", "question has already been answered")
		response = mustString(response, "learner response")
		insist(len(reasoning) <= 20000, "learner reasoning is too long")
		if selfConfidence != nil {
			insist(!math.IsNaN(*selfConfidence) && !math.IsInf(*selfConfidence, 0) && *selfConfidence >= 0 && *selfConfidence <= 1, "self-confidence must be between 0 and 1")
		}
		if strVal(q["response_kind"]) == "multiple_choice" {
			var opts []QuestionOption
			insist(jsonUnmarshal(strVal(q["options_json"]), &opts) == nil, "invalid stored question options")
			ok := false
			for _, opt := range opts {
				if opt.ID == response {
					ok = true
				}
			}
			insist(ok, "answer must match one of the listed option IDs")
		}
		exec(db, "UPDATE learning_questions SET status='answered',learner_response=?,assisted=?,answered_at=? WHERE id=?", response, assisted, now(), id)
		if reasoning != "" || selfConfidence != nil {
			var confidence any
			if selfConfidence != nil {
				confidence = *selfConfidence
			}
			exec(db, "INSERT INTO question_reflections(question_id,learner_reasoning,self_confidence,recorded_at) VALUES(?,?,?,?)", id, reasoning, confidence, now())
		}
		bumpSession(db, strVal(q["session_id"]))
		return questionPublic(db, id)
	})
}
func (s *Store) GradeLearningQuestion(id, verdict, evaluation string, confidence float64) (Value, error) {
	return s.GradeLearningQuestionWithReasoning(id, verdict, evaluation, confidence, "", "")
}

// GradeLearningQuestionWithReasoning evaluates correctness and the learner's
// argument separately. A correct selected option is not evidence of an
// understood inference unless its explanation was also examined.
func (s *Store) GradeLearningQuestionWithReasoning(id, verdict, evaluation string, confidence float64, reasoningVerdict, reasoningEvaluation string) (Value, error) {
	return s.with(func(db *database) Value {
		q := ensureRow(getRow(db, "SELECT * FROM learning_questions WHERE id=?", id), "learning question", id)
		insist(strVal(q["status"]) == "answered", "question must have a learner answer before assessment")
		insist(valid(verdict, "supported", "gap", "uncertain"), "invalid question verdict")
		if strVal(q["response_kind"]) == "multiple_choice" {
			correct := strVal(q["learner_response"]) == strVal(q["expected_answer"])
			insist((correct && verdict == "supported") || (!correct && verdict == "gap"), "multiple-choice verdict conflicts with the precommitted correct answer")
		}
		insist(!math.IsNaN(confidence) && !math.IsInf(confidence, 0) && confidence >= 0 && confidence <= 1, "confidence must be between 0 and 1")
		evaluation = mustString(evaluation, "evaluation")
		if reasoningVerdict != "" {
			insist(valid(reasoningVerdict, "supported", "gap", "uncertain"), "invalid reasoning verdict")
			reasoningEvaluation = mustString(reasoningEvaluation, "reasoning evaluation")
			learnerReasoning := strVal(q["learner_response"])
			if strVal(q["response_kind"]) == "multiple_choice" {
				learnerReasoning = ""
				reflection := getRow(db, "SELECT learner_reasoning FROM question_reflections WHERE question_id=?", id)
				if reflection != nil {
					learnerReasoning = strVal(reflection["learner_reasoning"])
				}
			}
			insist(strings.TrimSpace(learnerReasoning) != "", "cannot assess reasoning without a learner explanation")
		} else {
			insist(strings.TrimSpace(reasoningEvaluation) == "", "reasoning evaluation requires a reasoning verdict")
		}
		exec(db, "UPDATE learning_questions SET status='graded',verdict=?,evaluation=?,confidence=?,graded_at=? WHERE id=?", verdict, evaluation, confidence, now(), id)
		if reasoningVerdict != "" {
			exec(db, "INSERT INTO question_reasoning_reviews(question_id,verdict,evaluation,graded_at) VALUES(?,?,?,?)", id, reasoningVerdict, reasoningEvaluation, now())
		}
		if strVal(q["kind"]) == "diagnostic" {
			// A correct diagnostic answer may be a guess. Without an assessed
			// explanation, do not treat it as a supported prerequisite.
			probeVerdict := verdict
			if verdict == "supported" && reasoningVerdict != "supported" {
				probeVerdict = "uncertain"
				if reasoningVerdict == "gap" {
					probeVerdict = "gap"
				}
			}
			exec(db, `INSERT INTO session_probes(id,session_id,concept_id,question,learner_response,evaluation,outcome,confidence,assisted,recorded_at)
				VALUES(?,?,?,?,?,?,?,?,?,?)`, uuid(), q["session_id"], q["concept_id"], q["prompt"], q["learner_response"], evaluation, probeVerdict, confidence, q["assisted"], now())
		}
		bumpSession(db, strVal(q["session_id"]))
		out := questionPublic(db, id)
		out["mastery_unchanged"] = true
		return out
	})
}
