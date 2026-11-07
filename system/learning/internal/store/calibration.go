package store

// Calibration compares the learner's own probability of being correct with
// outcomes established by a sealed choice key or an authenticated checker.
// It does not infer correctness from model confidence or award mastery.

type calibrationSample struct {
	id         string
	domain     string
	confidence float64
	correct    bool
}

func summarizeCalibration(samples []calibrationSample, examined, missingConfidence, excludedConflicts int) Value {
	type bucket struct {
		name string
		min  float64
		max  float64
	}
	buckets := []bucket{
		{name: "low", min: 0, max: 0.5},
		{name: "medium", min: 0.5, max: 0.8},
		{name: "high", min: 0.8, max: 1.01},
	}
	bins := make([]Value, 0, len(buckets))
	var sumConfidence, sumCorrect, sumSquaredError float64
	highConfidenceErrors := []Value{}
	underconfidentSuccesses := []Value{}
	for _, sample := range samples {
		actual := 0.0
		if sample.correct {
			actual = 1
		}
		sumConfidence += sample.confidence
		sumCorrect += actual
		difference := sample.confidence - actual
		sumSquaredError += difference * difference
		if sample.confidence >= 0.8 && !sample.correct && len(highConfidenceErrors) < 10 {
			highConfidenceErrors = append(highConfidenceErrors, Value{"id": sample.id, "domain": sample.domain, "self_confidence": sample.confidence})
		}
		if sample.confidence < 0.5 && sample.correct && len(underconfidentSuccesses) < 10 {
			underconfidentSuccesses = append(underconfidentSuccesses, Value{"id": sample.id, "domain": sample.domain, "self_confidence": sample.confidence})
		}
	}
	for _, bucket := range buckets {
		count := 0
		var confidence, accuracy float64
		for _, sample := range samples {
			if sample.confidence < bucket.min || sample.confidence >= bucket.max {
				continue
			}
			count++
			confidence += sample.confidence
			if sample.correct {
				accuracy++
			}
		}
		result := Value{"band": bucket.name, "count": count, "mean_confidence": nil, "accuracy": nil}
		if count > 0 {
			result["mean_confidence"] = confidence / float64(count)
			result["accuracy"] = accuracy / float64(count)
		}
		bins = append(bins, result)
	}
	result := Value{
		"sample_count": len(samples), "eligible_examined": examined,
		"missing_self_confidence":     missingConfidence,
		"conflicting_checks_excluded": excludedConflicts,
		"mean_confidence":             nil, "observed_accuracy": nil, "brier_score": nil,
		"calibration_gap": nil, "insufficient_samples": len(samples) < 10,
		"bands": bins, "confident_errors": highConfidenceErrors,
		"underconfident_successes": underconfidentSuccesses,
	}
	if len(samples) > 0 {
		n := float64(len(samples))
		result["mean_confidence"] = sumConfidence / n
		result["observed_accuracy"] = sumCorrect / n
		result["brier_score"] = sumSquaredError / n
		result["calibration_gap"] = (sumConfidence - sumCorrect) / n
	}
	return result
}

// ConfidenceCalibration uses only outcomes whose correctness is determined
// independently of the model's evaluation. It keeps multiple-choice accuracy
// separate from the narrower scope of authenticated checker reports.
func (s *Store) ConfidenceCalibration(conceptID string, limit int) (Value, error) {
	return s.with(func(db *database) Value {
		insist(limit >= 1 && limit <= 500, "limit must be between 1 and 500")
		if conceptID != "" {
			ensureRow(getRow(db, "SELECT id FROM concepts WHERE id=?", conceptID), "concept", conceptID)
		}
		choiceRows := getRows(db, `SELECT q.id, c.domain, q.learner_response, q.expected_answer, qr.self_confidence
			FROM learning_questions q
			JOIN concepts c ON c.id=q.concept_id
			LEFT JOIN question_reflections qr ON qr.question_id=q.id
			WHERE q.status='graded' AND q.response_kind='multiple_choice' AND q.assisted=0
			  AND (?='' OR q.concept_id=?)
			ORDER BY q.graded_at DESC,q.rowid DESC LIMIT ?`, conceptID, conceptID, limit)
		choices := make([]calibrationSample, 0, len(choiceRows))
		missingChoices := 0
		for _, r := range choiceRows {
			if r["self_confidence"] == nil {
				missingChoices++
				continue
			}
			choices = append(choices, calibrationSample{
				id: strVal(r["id"]), domain: strVal(r["domain"]),
				confidence: r["self_confidence"].(float64),
				correct:    strVal(r["learner_response"]) == strVal(r["expected_answer"]),
			})
		}
		problemRows := getRows(db, `SELECT p.id,p.discipline AS domain,r.self_confidence,
			    SUM(CASE WHEN pc.outcome='pass' THEN 1 ELSE 0 END) AS passes,
			    SUM(CASE WHEN pc.outcome='fail' THEN 1 ELSE 0 END) AS failures
			FROM problems p
			JOIN problem_drafts d ON d.problem_id=p.id AND
			    d.rowid=(SELECT MAX(other.rowid) FROM problem_drafts other WHERE other.problem_id=p.id)
			LEFT JOIN problem_draft_reflections r ON r.draft_id=d.id
			JOIN problem_checks pc ON pc.draft_id=d.id
			JOIN verified_problem_checks vc ON vc.check_id=pc.id
			WHERE p.status!='open'
			    AND (?='' OR EXISTS(SELECT 1 FROM problem_concepts c
			       WHERE c.problem_id=p.id AND c.concept_id=?))
			GROUP BY p.id,p.discipline,r.self_confidence
			HAVING passes+failures>0
			ORDER BY p.completed_at DESC,p.rowid DESC LIMIT ?`, conceptID, conceptID, limit)
		verified := make([]calibrationSample, 0, len(problemRows))
		missingProblems, conflicts := 0, 0
		for _, r := range problemRows {
			passes := intVal(r["passes"])
			failures := intVal(r["failures"])
			if passes > 0 && failures > 0 {
				conflicts++
				continue
			}
			if r["self_confidence"] == nil {
				missingProblems++
				continue
			}
			verified = append(verified, calibrationSample{
				id: strVal(r["id"]), domain: strVal(r["domain"]),
				confidence: r["self_confidence"].(float64),
				correct:    passes > 0,
			})
		}
		return Value{
			"concept_id": conceptID, "limit_per_source": limit,
			"choice_accuracy":               summarizeCalibration(choices, len(choiceRows), missingChoices, 0),
			"authenticated_result_accuracy": summarizeCalibration(verified, len(problemRows), missingProblems, conflicts),
			"mastery_unchanged":             true,
			"limitations":                   "Choice accuracy assumes the precommitted option key is correct. Signed checker outcomes cover only the verifier's stated scope, not all reasoning. Self-confidence is optional. Scores are descriptive, not evidence of general intelligence or mastery; fewer than ten observations are insufficient for reliable interpretation.",
		}
	})
}
